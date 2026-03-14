package sfu

import (
	"io"
	"log"
	"sync"
	"time"

	"Remainwith/internal/sfu/pubsub"
	"Remainwith/internal/transport"

	"github.com/pion/rtcp"
)

// PeerManager manages peers in a room
type PeerManager struct {
	roomID string
	mu     sync.RWMutex
	wg     sync.WaitGroup

	// Transports indexed by ClientID
	transports map[string]transport.Transport

	// PubSub for track management
	pubsub *pubsub.PubSub

	// PLI times for congestion control
	pliTimes map[string]time.Time

	// Phase 8: Track readers registry to prevent duplicates
	// Maps trackID to its reader to ensure single reader per track
	trackReaders map[string]bool
}

// NewPeerManager creates a new peer manager for a room
func NewPeerManager(roomID string) *PeerManager {
	return &PeerManager{
		roomID:       roomID,
		transports:   make(map[string]transport.Transport),
		pubsub:       pubsub.New(),
		pliTimes:     make(map[string]time.Time),
		trackReaders: make(map[string]bool),
	}
}

// Add adds a transport to the peer manager
func (pm *PeerManager) Add(tr transport.Transport) (<-chan pubsub.PubTrackEvent, error) {
	clientID := tr.ClientID()

	pm.mu.Lock()

	// Remove existing transport if present
	if existing, ok := pm.transports[clientID]; ok {
		existing.Close()
		pm.remove(clientID)
	}

	// Subscribe to events
	eventCh, err := pm.pubsub.SubscribeToEvents(clientID)
	if err != nil {
		pm.mu.Unlock()
		return nil, err
	}

	// Add transport
	pm.transports[clientID] = tr

	pm.mu.Unlock()

	// Create output channel
	pubTrackEventsCh := make(chan pubsub.PubTrackEvent)

	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()

		// Send existing tracks to new peer
		pm.mu.RLock()
		existingTracks := pm.pubsub.Tracks()
		pm.mu.RUnlock()

		for _, track := range existingTracks {
			if track.ClientID != clientID {
				select {
				case pubTrackEventsCh <- pubsub.PubTrackEvent{
					PubTrack: track,
					Type:     pubsub.TrackEventTypeAdd,
				}:
				case <-time.After(time.Second):
					log.Printf("PeerManager: Timeout sending existing track to %s", clientID)
				}
			}
		}

		// Forward events
		for event := range eventCh {
			if event.PubTrack.ClientID != clientID {
				select {
				case pubTrackEventsCh <- event:
				case <-time.After(time.Second):
					log.Printf("PeerManager: Timeout forwarding event to %s", clientID)
				}
			}
		}
	}()

	// Handle remote tracks
	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()

		remoteTracksCh := tr.RemoteTracksChannel()
		doneCh := tr.Done()

		trackReceiveTimeout := time.NewTimer(30 * time.Second)
		trackReceived := false

		log.Printf("PeerManager: [TRACK RECEIVER] STARTED for client %s - waiting for tracks from channel", clientID)

		for {
			select {
			case trackWithReader := <-remoteTracksCh:
				trackReceiveTimeout.Stop() // Stop timeout once we receive a track
				trackReceived = true

				log.Printf("PeerManager: [TRACK RECEIVER] RECEIVED track from channel for client %s", clientID)

				if trackWithReader.TrackRemote == nil {
					log.Printf("PeerManager: [TRACK RECEIVER] Received nil track from client %s, skipping", clientID)
					continue
				}
				track := trackWithReader.TrackRemote
				if track.Track() == nil {
					log.Printf("PeerManager: [TRACK RECEIVER] Received track with nil Track() from client %s, skipping", clientID)
					continue
				}
				trackID := track.Track().ID()
				trackKind := track.Track().Kind().String()

				log.Printf("PeerManager: [TRACK RECEIVER] Processing track %s (kind=%s) from client %s", trackID, trackKind, clientID)

				// Phase 8: Check for duplicate track readers
				pm.mu.Lock()
				if pm.trackReaders[trackID] {
					// Already have a reader for this track, skip
					log.Printf("PeerManager: [TRACK RECEIVER] DUPLICATE track reader detected for %s, skipping", trackID)
					pm.mu.Unlock()
					continue
				}
				// Mark this track as having a reader
				pm.trackReaders[trackID] = true
				pm.mu.Unlock()

				log.Printf("PeerManager: [TRACK RECEIVER] Track %s (%s) from client %s - passed duplicate check", trackID, trackKind, clientID)

				// CRITICAL FIX: Handle each track in its own goroutine so the select loop
				// is not blocked and can immediately receive the next track (e.g., video
				// arriving right after audio).
				capturedTrack := trackWithReader
				capturedTrackID := trackID
				capturedTrackKind := trackKind
				pm.wg.Add(1)
				go func() {
					defer pm.wg.Done()

					// Create done channel for cleanup
					done := make(chan struct{})

					// Publish track to pubsub - this will start forwarding to all subscribers
					reader := pubsub.NewTrackReader(capturedTrack.TrackRemote, capturedTrack.Codec, func() {
						close(done)
						// Phase 8: Clean up track reader registry
						pm.mu.Lock()
						delete(pm.trackReaders, capturedTrackID)
						pm.mu.Unlock()
						pm.pubsub.Unpub(clientID, capturedTrackID)
					})

					// Publish the track - this notifies all subscribers and starts forwarding
					pm.mu.Lock()
					log.Printf("PeerManager: [TRACK RECEIVER] Calling pubsub.Pub for track %s (kind=%s) from client %s", capturedTrackID, capturedTrackKind, clientID)
					if err := pm.pubsub.Pub(clientID, reader); err != nil {
						log.Printf("PeerManager: [TRACK RECEIVER] ERROR publishing track %s: %v", capturedTrackID, err)
						pm.mu.Unlock()
						return
					}
					pm.mu.Unlock()

					log.Printf("PeerManager: [TRACK RECEIVER] SUCCESSFULLY published track %s (%s) from client %s to pubsub", capturedTrackID, capturedTrackKind, clientID)

					// Phase 8: Single RTCP processing - no duplicate readers
					pm.wg.Add(1)
					go func() {
						defer pm.wg.Done()

						for {
							packets, _, err := capturedTrack.RTCPReader.ReadRTCP()

							if err != nil {
								if err != io.EOF {
									log.Printf("PeerManager: Error reading RTCP for track %s: %v", capturedTrackID, err)
								}
								return
							}

							for _, packet := range packets {
								switch p := packet.(type) {
								case *rtcp.PictureLossIndication:
									log.Printf("PeerManager: Received PLI for track %s", capturedTrackID)
									pm.forwardPLI(clientID, capturedTrackID, p)
								case *rtcp.ReceiverEstimatedMaximumBitrate:
									log.Printf("PeerManager: Received REMB for track %s: %f", capturedTrackID, p.Bitrate)
									// Update bitrate estimator
									if estimator, ok := pm.pubsub.BitrateEstimator(capturedTrackID); ok {
										estimator.Feed(clientID, p.Bitrate)
									}
								}
							}
						}
					}()

					// Wait for track cleanup
					<-done
				}()

			case <-trackReceiveTimeout.C:
				if !trackReceived {
					log.Printf("PeerManager: WARNING - No tracks received from client %s within 30 seconds. Check if browser is sending media tracks. Media may not work.", clientID)
				}

			case <-doneCh:
				trackReceiveTimeout.Stop()
				return
			}
		}
	}()

	// Handle data channel messages
	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()

		messagesCh := tr.MessagesChannel()
		doneCh := tr.Done()

		for {
			select {
			case msg := <-messagesCh:
				pm.broadcast(clientID, msg)
			case <-doneCh:
				return
			}
		}
	}()

	return pubTrackEventsCh, nil
}

// broadcast broadcasts a data channel message to all other peers
func (pm *PeerManager) broadcast(senderID string, msg interface{}) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for clientID, tr := range pm.transports {
		if clientID != senderID {
			// Send message asynchronously
			go func(id string, t transport.Transport) {
				// Note: This is a simplified version. In production, you'd want
				// to handle the actual data channel message properly
				log.Printf("PeerManager: Broadcasting message from %s to %s", senderID, id)
			}(clientID, tr)
		}
	}
}

// forwardPLI forwards a PLI packet to the source of a track
func (pm *PeerManager) forwardPLI(requesterID, trackID string, pli *rtcp.PictureLossIndication) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Get track properties
	track, ok := pm.pubsub.TrackPropsByTrackID(trackID)
	if !ok {
		log.Printf("PeerManager: Track not found for PLI: %s", trackID)
		return
	}

	// Get source transport
	sourceTransport, ok := pm.transports[track.ClientID]
	if !ok {
		log.Printf("PeerManager: Source transport not found for track: %s", trackID)
		return
	}

	// Check PLI timing for congestion control
	now := time.Now()
	lastPLI := pm.pliTimes[trackID]
	if now.Sub(lastPLI) < time.Second {
		// Too soon, ignore
		return
	}
	pm.pliTimes[trackID] = now

	// Set correct SSRC
	pli.MediaSSRC = uint32(track.Reader.SSRC())
	pli.SenderSSRC = uint32(track.Reader.SSRC())

	// Send PLI to source
	if err := sourceTransport.WriteRTCP([]rtcp.Packet{pli}); err != nil {
		log.Printf("PeerManager: Error forwarding PLI: %v", err)
	} else {
		log.Printf("PeerManager: Forwarded PLI to %s for track %s", track.ClientID, trackID)
	}
}

// Sub subscribes a client to a track
func (pm *PeerManager) Sub(pubClientID, trackID, subClientID string, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Add subscription
	if err := pm.pubsub.Sub(pubClientID, trackID, subClientID, writer, rtcpReader); err != nil {
		return err
	}

	// Phase 8: Single RTCP processing for subscriber
	// Start RTCP processing for this subscriber only once
	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()

		for {
			packets, _, err := rtcpReader.ReadRTCP()

			if err != nil {
				if err != io.EOF {
					log.Printf("PeerManager: Error reading subscriber RTCP: %v", err)
				}
				return
			}

			for _, packet := range packets {
				switch p := packet.(type) {
				case *rtcp.PictureLossIndication:
					log.Printf("PeerManager: Received PLI from subscriber %s for track %s", subClientID, trackID)
					pm.forwardPLI(subClientID, trackID, p)
				case *rtcp.ReceiverEstimatedMaximumBitrate:
					if estimator, ok := pm.pubsub.BitrateEstimator(trackID); ok {
						estimator.Feed(subClientID, p.Bitrate)
					}
				}
			}
		}
	}()

	return nil
}

// Unsub unsubscribes a client from a track
func (pm *PeerManager) Unsub(pubClientID, trackID, subClientID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return pm.pubsub.Unsub(pubClientID, trackID, subClientID)
}

// Remove removes a transport from the peer manager
func (pm *PeerManager) Remove(tr transport.Transport) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	clientID := tr.ClientID()

	if existing, ok := pm.transports[clientID]; !ok {
		return nil // Not found
	} else if existing != tr {
		return nil // Different transport, ignore
	}

	pm.remove(clientID)

	return nil
}

// remove removes a client and cleans up resources
func (pm *PeerManager) remove(clientID string) {
	// Unsubscribe from events
	pm.pubsub.UnsubscribeFromEvents(clientID)

	// Terminate all tracks and subscriptions for this client
	pm.pubsub.Terminate(clientID)

	// Remove from transports
	delete(pm.transports, clientID)

	log.Printf("PeerManager: Removed client %s from room %s", clientID, pm.roomID)
}

// Size returns the number of peers in the room
func (pm *PeerManager) Size() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return len(pm.transports)
}

// Close closes the peer manager and all transports
func (pm *PeerManager) Close() <-chan struct{} {
	pm.mu.Lock()

	// Close all transports
	for clientID, tr := range pm.transports {
		log.Printf("PeerManager: Closing transport for client %s", clientID)
		tr.Close()
		delete(pm.transports, clientID)
	}

	pm.mu.Unlock()

	ch := make(chan struct{})

	go func() {
		pm.wg.Wait()
		pm.pubsub.Close()
		close(ch)
	}()

	return ch
}
