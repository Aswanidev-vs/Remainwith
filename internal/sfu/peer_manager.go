package sfu

import (
	"io"
	"log"
	"sync"
	"time"

	"Remainwith/internal/sfu/congestion"
	"Remainwith/internal/sfu/jitter"
	"Remainwith/internal/sfu/pubsub"
	"Remainwith/internal/sfu/simulcast"
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

	// JitterHandler for packet loss recovery
	jitterHandler jitter.JitterHandler

	// Simulcast manager for layer selection
	simulcastManager *simulcast.Manager

	// Congestion controller for bandwidth adaptation
	congestionController *congestion.Controller
}

// NewPeerManager creates a new peer manager for a room
func NewPeerManager(roomID string) *PeerManager {
	sm := simulcast.NewManager()
	cc := congestion.NewController(sm, congestion.DefaultConfig())

	return &PeerManager{
		roomID:               roomID,
		transports:           make(map[string]transport.Transport),
		pubsub:               pubsub.New(),
		pliTimes:             make(map[string]time.Time),
		jitterHandler:        jitter.NewJitterHandler(true), // Enable jitter buffer
		simulcastManager:     sm,
		congestionController: cc,
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
				case <-time.After(40000 * time.Second):
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

		trackReceiveTimeout := time.NewTimer(15 * time.Second)
		trackReceived := false

		for {
			select {
			case trackWithReader := <-remoteTracksCh:
				trackReceiveTimeout.Stop() // Stop timeout once we receive a track
				trackReceived = true

				if trackWithReader.TrackRemote == nil {
					log.Printf("PeerManager: Received nil track from client %s, skipping", clientID)
					continue
				}
				track := trackWithReader.TrackRemote
				if track.Track() == nil {
					log.Printf("PeerManager: Received track with nil Track() from client %s, skipping", clientID)
					continue
				}
				trackID := track.Track().ID()

				log.Printf("PeerManager: Received remote track %s (%s) from client %s", trackID, track.Track().Kind().String(), clientID)

				// Create done channel for cleanup
				done := make(chan struct{})

				// Publish track
				reader := pubsub.NewTrackReader(track, func() {
					close(done)
					pm.pubsub.Unpub(clientID, trackID)
				})

				pm.mu.Lock()
				pm.pubsub.Pub(clientID, reader)

				// Auto-subscribe all other peers to this new track
				// This is critical for video sharing - when A publishes, B and C need to subscribe
				for otherClientID, otherTransport := range pm.transports {
					if otherClientID != clientID {
						// Add track to the other transport - this creates both the local track and RTCP reader
						trackLocal, rtcpReader, err := otherTransport.AddTrack(track.Track())
						if err != nil {
							log.Printf("PeerManager: Failed to add track for %s: %v", otherClientID, err)
							continue
						}

						// Subscribe the other peer to this track
						if err := pm.pubsub.Sub(clientID, trackID, otherClientID, trackLocal, rtcpReader); err != nil {
							log.Printf("PeerManager: Failed to auto-subscribe %s to track %s: %v", otherClientID, trackID, err)
						} else {
							log.Printf("PeerManager: Auto-subscribed %s to track %s from %s", otherClientID, trackID, clientID)
						}
					}
				}

				pm.mu.Unlock()

				// Process RTCP for this track
				pm.wg.Add(1)
				go func() {
					defer pm.wg.Done()

					for {
						packets, _, err := trackWithReader.RTCPReader.ReadRTCP()

						if err != nil {
							if err != io.EOF {
								log.Printf("PeerManager: Error reading RTCP for track %s: %v", trackID, err)
							}
							return
						}

						for _, packet := range packets {
							switch p := packet.(type) {
							case *rtcp.PictureLossIndication:
								log.Printf("PeerManager: Received PLI for track %s", trackID)
								pm.forwardPLI(clientID, trackID, p)
							case *rtcp.ReceiverEstimatedMaximumBitrate:
								log.Printf("PeerManager: Received REMB for track %s: %f", trackID, p.Bitrate)
								// Update bitrate estimator
								if estimator, ok := pm.pubsub.BitrateEstimator(trackID); ok {
									estimator.Feed(clientID, p.Bitrate)
								}
								// Update congestion controller with REMB bitrate
								pm.congestionController.UpdateBitrateEstimate(clientID, uint32(p.Bitrate))
							case *rtcp.TransportLayerNack:
								// Handle NACK - try to recover packets from jitter buffer
								log.Printf("PeerManager: Received NACK for track %s, SSRC %d", trackID, p.MediaSSRC)
								recoveredPackets, forwardNack := pm.jitterHandler.HandleNack(p)

								// Send recovered packets immediately
								for _, pkt := range recoveredPackets {
									log.Printf("PeerManager: Recovered packet %d from jitter buffer", pkt.SequenceNumber)
								}

								// Forward NACK to source if packets couldn't be recovered
								if forwardNack != nil {
									pm.forwardNACK(clientID, trackID, forwardNack)
								}
							case *rtcp.ReceiverReport:
								// Process receiver reports for congestion control
								for _, report := range p.Reports {
									// Use fraction lost (0-255, where 255 = 100% loss)
									if report.FractionLost > 0 {
										lossRate := float64(report.FractionLost) / 255.0
										pm.congestionController.UpdateLossRate(clientID, lossRate)
									}
									// Update RTT from delay
									if report.Delay != 0 {
										rtt := uint32(report.Delay) / 65536 * 1000 // Convert to ms
										pm.congestionController.UpdateRTT(clientID, rtt)
									}
								}
							}
						}
					}
				}()

				// NOTE: RTP packet processing (jitter buffer for NACK generation)
				// is now handled in pubsub.forwardTrack -> processAndForwardPacket.
				// We removed the duplicate reader here because calling track.ReadRTP()
				// from two goroutines causes a race condition where packets are lost.
				// The PubSub's jitter buffer will detect missing packets and generate NACKs.

				// Wait for track cleanup
				<-done

			case <-trackReceiveTimeout.C:
				if !trackReceived {
					log.Printf("PeerManager: WARNING - No tracks received from client %s within 15 seconds. Check if browser is sending media tracks. Media may not work.", clientID)
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

// forwardNACK forwards a NACK packet to the source of a track
func (pm *PeerManager) forwardNACK(requesterID, trackID string, nack *rtcp.TransportLayerNack) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Get track properties
	track, ok := pm.pubsub.TrackPropsByTrackID(trackID)
	if !ok {
		log.Printf("PeerManager: Track not found for NACK: %s", trackID)
		return
	}

	// Get source transport
	sourceTransport, ok := pm.transports[track.ClientID]
	if !ok {
		log.Printf("PeerManager: Source transport not found for track: %s", trackID)
		return
	}

	// Send NACK to source
	if err := sourceTransport.WriteRTCP([]rtcp.Packet{nack}); err != nil {
		log.Printf("PeerManager: Error forwarding NACK: %v", err)
	} else {
		log.Printf("PeerManager: Forwarded NACK to %s for track %s", track.ClientID, trackID)
	}
}

// GetJitterStats returns jitter buffer statistics for monitoring
func (pm *PeerManager) GetJitterStats() map[uint32]struct {
	Received uint64
	Lost     uint64
} {
	if handler, ok := pm.jitterHandler.(*jitter.NackHandler); ok {
		return handler.GetStats()
	}
	return nil
}

// Sub subscribes a client to a track
func (pm *PeerManager) Sub(pubClientID, trackID, subClientID string, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Add subscriber to congestion control if not already added
	pm.congestionController.AddSubscriber(subClientID)

	// Select optimal simulcast layer for this subscriber
	optimalLayer := pm.simulcastManager.SelectOptimalLayer(subClientID, trackID)
	log.Printf("PeerManager: Selected layer %s for subscriber %s on track %s", optimalLayer.String(), subClientID, trackID)

	// Add subscription
	if err := pm.pubsub.Sub(pubClientID, trackID, subClientID, writer, rtcpReader); err != nil {
		return err
	}

	// Start RTCP processing for subscriber
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
					// Update congestion controller with subscriber's REMB
					pm.congestionController.UpdateBitrateEstimate(subClientID, uint32(p.Bitrate))
				case *rtcp.ReceiverReport:
					// Update RTT for congestion control
					for _, report := range p.Reports {
						if report.Delay != 0 {
							// Calculate RTT from delay
							rtt := uint32(report.Delay) / 65536 * 1000 // Convert to ms
							pm.congestionController.UpdateRTT(subClientID, rtt)
						}
						// Update loss rate
						if report.FractionLost > 0 {
							lossRate := float64(report.FractionLost) / 255.0
							pm.congestionController.UpdateLossRate(subClientID, lossRate)
						}
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

	// Remove from congestion control
	pm.congestionController.RemoveSubscriber(clientID)

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
		pm.simulcastManager.Close()
		pm.congestionController.Close()
		close(ch)
	}()

	return ch
}

// GetSimulcastManager returns the simulcast manager
func (pm *PeerManager) GetSimulcastManager() *simulcast.Manager {
	return pm.simulcastManager
}

// GetCongestionController returns the congestion controller
func (pm *PeerManager) GetCongestionController() *congestion.Controller {
	return pm.congestionController
}
