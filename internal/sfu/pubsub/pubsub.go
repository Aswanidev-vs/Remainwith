// Package pubsub provides publish/subscribe functionality for media tracks
package pubsub

import (
	"fmt"
	"log"
	"sync"
	"time"

	"Remainwith/internal/transport"

	"github.com/pion/rtp"
)

// PubTrack represents a published track
type PubTrack struct {
	ClientID string
	TrackID  string
	PeerID   string
	Kind     string
	Reader   transport.TrackRemote
}

// TrackReader wraps a track remote with cleanup functionality
type TrackReader struct {
	track   transport.TrackRemote
	onClose func()
}

// NewTrackReader creates a new track reader
func NewTrackReader(track transport.TrackRemote, onClose func()) *TrackReader {
	return &TrackReader{
		track:   track,
		onClose: onClose,
	}
}

// ReadRTP reads RTP packets from the track
func (t *TrackReader) ReadRTP() (*rtp.Packet, error) {
	packet, _, err := t.track.ReadRTP()
	return packet, err
}

// Close closes the track reader
func (t *TrackReader) Close() {
	if t.onClose != nil {
		t.onClose()
	}
}

// TrackEventType represents the type of track event
type TrackEventType string

const (
	// TrackEventTypeAdd indicates a track was added
	TrackEventTypeAdd TrackEventType = "add"
	// TrackEventTypeRemove indicates a track was removed
	TrackEventTypeRemove TrackEventType = "remove"
	// TrackEventTypeSub indicates a track was subscribed
	TrackEventTypeSub TrackEventType = "sub"
	// TrackEventTypeUnsub indicates a track was unsubscribed
	TrackEventTypeUnsub TrackEventType = "unsub"
)

// PubTrackEvent represents a track publication event
type PubTrackEvent struct {
	PubTrack PubTrack
	Type     TrackEventType
}

// Sub represents a subscription to a track
type Sub struct {
	ClientID   string
	Writer     transport.TrackLocal
	RTCPReader transport.RTCPReader
}

// PubSub manages track publication and subscription
type PubSub struct {
	mu sync.RWMutex

	// Published tracks indexed by clientID -> trackID
	publishedTracks map[string]map[string]*PubTrack

	// Subscriptions indexed by clientID -> trackID -> subscriberClientID
	subscriptions map[string]map[string]map[string]*Sub

	// Event subscribers indexed by clientID
	eventSubscribers map[string]chan PubTrackEvent

	// Track readers for reading RTP packets
	trackReaders map[string]*TrackReader

	// Bitrate estimators
	bitrateEstimators map[string]*BitrateEstimator

	// Cleanup ticker
	cleanupTicker *time.Ticker
	done          chan struct{}
}

// BitrateEstimator estimates bitrate for a track
type BitrateEstimator struct {
	mu       sync.RWMutex
	bitrates map[string]float32
}

// NewBitrateEstimator creates a new bitrate estimator
func NewBitrateEstimator() *BitrateEstimator {
	return &BitrateEstimator{
		bitrates: make(map[string]float32),
	}
}

// Feed feeds a bitrate sample
func (b *BitrateEstimator) Feed(clientID string, bitrate float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bitrates[clientID] = bitrate
}

// Min returns the minimum bitrate
func (b *BitrateEstimator) Min() float32 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var min float32 = -1
	for _, bitrate := range b.bitrates {
		if min < 0 || bitrate < min {
			min = bitrate
		}
	}
	return min
}

// Empty returns true if no bitrates have been recorded
func (b *BitrateEstimator) Empty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.bitrates) == 0
}

// New creates a new PubSub instance
func New() *PubSub {
	ps := &PubSub{
		publishedTracks:   make(map[string]map[string]*PubTrack),
		subscriptions:     make(map[string]map[string]map[string]*Sub),
		eventSubscribers:  make(map[string]chan PubTrackEvent),
		trackReaders:      make(map[string]*TrackReader),
		bitrateEstimators: make(map[string]*BitrateEstimator),
		cleanupTicker:     time.NewTicker(30 * time.Second),
		done:              make(chan struct{}),
	}

	go ps.cleanupLoop()

	return ps
}

// cleanupLoop periodically cleans up stale resources
func (ps *PubSub) cleanupLoop() {
	for {
		select {
		case <-ps.cleanupTicker.C:
			ps.cleanup()
		case <-ps.done:
			return
		}
	}
}

// cleanup removes stale subscriptions and tracks
func (ps *PubSub) cleanup() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Clean up empty subscription maps
	for clientID, tracks := range ps.subscriptions {
		for trackID, subs := range tracks {
			if len(subs) == 0 {
				delete(tracks, trackID)
			}
		}
		if len(tracks) == 0 {
			delete(ps.subscriptions, clientID)
		}
	}
}

// Pub publishes a track
func (ps *PubSub) Pub(clientID string, reader *TrackReader) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	track := reader.track
	trackID := track.Track().ID()

	// Initialize published tracks map for client if needed
	if ps.publishedTracks[clientID] == nil {
		ps.publishedTracks[clientID] = make(map[string]*PubTrack)
	}

	// Create pub track
	pubTrack := &PubTrack{
		ClientID: clientID,
		TrackID:  trackID,
		PeerID:   clientID, // peerID is same as clientID for WebRTC
		Kind:     track.Track().Kind().String(),
		Reader:   track,
	}

	ps.publishedTracks[clientID][trackID] = pubTrack
	ps.trackReaders[trackID] = reader

	// Initialize bitrate estimator
	ps.bitrateEstimators[trackID] = NewBitrateEstimator()

	// Notify subscribers
	ps.notifyEventSubscribers(PubTrackEvent{
		PubTrack: *pubTrack,
		Type:     TrackEventTypeAdd,
	})

	log.Printf("PubSub: Track published - clientID: %s, trackID: %s", clientID, trackID)

	// Start forwarding RTP packets
	go ps.forwardTrack(clientID, trackID, reader)

	return nil
}

// Unpub unpublishes a track
func (ps *PubSub) Unpub(clientID, trackID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Remove from published tracks
	if tracks, ok := ps.publishedTracks[clientID]; ok {
		if pubTrack, ok := tracks[trackID]; ok {
			delete(tracks, trackID)

			// Notify subscribers
			ps.notifyEventSubscribers(PubTrackEvent{
				PubTrack: *pubTrack,
				Type:     TrackEventTypeRemove,
			})
		}

		if len(tracks) == 0 {
			delete(ps.publishedTracks, clientID)
		}
	}

	// Close track reader
	if reader, ok := ps.trackReaders[trackID]; ok {
		reader.Close()
		delete(ps.trackReaders, trackID)
	}

	// Remove bitrate estimator
	delete(ps.bitrateEstimators, trackID)

	// Remove all subscriptions for this track
	delete(ps.subscriptions[clientID], trackID)

	log.Printf("PubSub: Track unpublished - clientID: %s, trackID: %s", clientID, trackID)
}

// Sub subscribes to a track
func (ps *PubSub) Sub(pubClientID, trackID, subClientID string, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if track exists
	tracks, ok := ps.publishedTracks[pubClientID]
	if !ok {
		return fmt.Errorf("no tracks found for client: %s", pubClientID)
	}

	pubTrack, ok := tracks[trackID]
	if !ok {
		return fmt.Errorf("track not found: %s", trackID)
	}

	// Initialize subscriptions map
	if ps.subscriptions[pubClientID] == nil {
		ps.subscriptions[pubClientID] = make(map[string]map[string]*Sub)
	}
	if ps.subscriptions[pubClientID][trackID] == nil {
		ps.subscriptions[pubClientID][trackID] = make(map[string]*Sub)
	}

	// Add subscription
	ps.subscriptions[pubClientID][trackID][subClientID] = &Sub{
		ClientID:   subClientID,
		Writer:     writer,
		RTCPReader: rtcpReader,
	}

	// Notify
	ps.notifyEventSubscribers(PubTrackEvent{
		PubTrack: *pubTrack,
		Type:     TrackEventTypeSub,
	})

	log.Printf("PubSub: Track subscribed - pubClientID: %s, trackID: %s, subClientID: %s", pubClientID, trackID, subClientID)

	return nil
}

// Unsub unsubscribes from a track
func (ps *PubSub) Unsub(pubClientID, trackID, subClientID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if subscriptions exist
	tracks, ok := ps.subscriptions[pubClientID]
	if !ok {
		return nil // No subscriptions for this client
	}

	subs, ok := tracks[trackID]
	if !ok {
		return nil // No subscriptions for this track
	}

	// Remove subscription
	delete(subs, subClientID)

	// Notify if track exists
	if pubTracks, ok := ps.publishedTracks[pubClientID]; ok {
		if pubTrack, ok := pubTracks[trackID]; ok {
			ps.notifyEventSubscribers(PubTrackEvent{
				PubTrack: *pubTrack,
				Type:     TrackEventTypeUnsub,
			})
		}
	}

	log.Printf("PubSub: Track unsubscribed - pubClientID: %s, trackID: %s, subClientID: %s", pubClientID, trackID, subClientID)

	return nil
}

// SubscribeToEvents subscribes to track events for a client
func (ps *PubSub) SubscribeToEvents(clientID string) (<-chan PubTrackEvent, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, ok := ps.eventSubscribers[clientID]; ok {
		return nil, fmt.Errorf("client %s already subscribed to events", clientID)
	}

	ch := make(chan PubTrackEvent, 10)
	ps.eventSubscribers[clientID] = ch

	return ch, nil
}

// UnsubscribeFromEvents unsubscribes from track events
func (ps *PubSub) UnsubscribeFromEvents(clientID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ch, ok := ps.eventSubscribers[clientID]; ok {
		close(ch)
		delete(ps.eventSubscribers, clientID)
	}

	return nil
}

// notifyEventSubscribers notifies all event subscribers
func (ps *PubSub) notifyEventSubscribers(event PubTrackEvent) {
	for _, ch := range ps.eventSubscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// forwardTrack forwards RTP packets from a track to all subscribers
func (ps *PubSub) forwardTrack(clientID, trackID string, reader *TrackReader) {
	// Generate a unique SSRC for this track to avoid conflicts
	// Each track gets a deterministic SSRC based on trackID hash
	trackSSRC := generateTrackSSRC(trackID)

	for {
		packet, err := reader.ReadRTP()

		if err != nil {
			log.Printf("PubSub: Error reading RTP from track %s: %v", trackID, err)
			return
		}

		ps.mu.RLock()
		subs, ok := ps.subscriptions[clientID][trackID]
		ps.mu.RUnlock()

		if !ok || len(subs) == 0 {
			continue
		}

		// Rewrite SSRC to our unique track SSRC
		// This ensures all subscribers see consistent SSRC values
		originalSSRC := packet.SSRC
		packet.SSRC = trackSSRC

		// Forward to all subscribers
		for _, sub := range subs {
			if err := sub.Writer.WriteRTP(packet); err != nil {
				log.Printf("PubSub: Error writing RTP to subscriber %s: %v", sub.ClientID, err)
			}
		}

		// Restore original SSRC for logging/debugging if needed
		packet.SSRC = originalSSRC
	}
}

// generateTrackSSRC generates a unique SSRC for a track
// SSRC is a 32-bit value, we use a hash of the trackID
func generateTrackSSRC(trackID string) uint32 {
	var hash uint32 = 0
	for i, c := range trackID {
		hash = hash*31 + uint32(c) + uint32(i)
	}
	// Ensure SSRC is valid (non-zero and fits in 32 bits)
	if hash == 0 {
		hash = 1
	}
	// Use upper 31 bits to avoid potential conflicts
	return hash & 0x7FFFFFFF
}

// Tracks returns all published tracks
func (ps *PubSub) Tracks() []PubTrack {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var tracks []PubTrack
	for _, clientTracks := range ps.publishedTracks {
		for _, track := range clientTracks {
			tracks = append(tracks, *track)
		}
	}
	return tracks
}

// BitrateEstimator returns the bitrate estimator for a track
func (ps *PubSub) BitrateEstimator(trackID string) (*BitrateEstimator, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	estimator, ok := ps.bitrateEstimators[trackID]
	return estimator, ok
}

// TrackPropsByTrackID returns track properties by track ID
func (ps *PubSub) TrackPropsByTrackID(trackID string) (PubTrack, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for _, clientTracks := range ps.publishedTracks {
		if track, ok := clientTracks[trackID]; ok {
			return *track, true
		}
	}

	return PubTrack{}, false
}

// Terminate terminates all subscriptions for a client
func (ps *PubSub) Terminate(clientID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Unpublish all tracks
	if tracks, ok := ps.publishedTracks[clientID]; ok {
		for trackID := range tracks {
			// Close track reader
			if reader, ok := ps.trackReaders[trackID]; ok {
				reader.Close()
				delete(ps.trackReaders, trackID)
			}

			// Remove bitrate estimator
			delete(ps.bitrateEstimators, trackID)
		}
		delete(ps.publishedTracks, clientID)
	}

	// Remove all subscriptions by this client
	for pubClientID, tracks := range ps.subscriptions {
		for trackID, subs := range tracks {
			delete(subs, clientID)
			if len(subs) == 0 {
				delete(tracks, trackID)
			}
		}
		if len(tracks) == 0 {
			delete(ps.subscriptions, pubClientID)
		}
	}

	// Remove event subscriber
	if ch, ok := ps.eventSubscribers[clientID]; ok {
		close(ch)
		delete(ps.eventSubscribers, clientID)
	}
}

// Close closes the PubSub
func (ps *PubSub) Close() {
	close(ps.done)
	ps.cleanupTicker.Stop()
}
