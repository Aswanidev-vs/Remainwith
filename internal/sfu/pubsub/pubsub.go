// Package pubsub provides publish/subscribe functionality for media tracks
package pubsub

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"Remainwith/internal/sfu/jitter"
<<<<<<< HEAD
	"Remainwith/internal/sfu/recorder"
=======
>>>>>>> main
	"Remainwith/internal/transport"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// PubTrack represents a published track
type PubTrack struct {
	ClientID string
	TrackID  string
	PeerID   string
	Kind     string
	Codec    string // MimeType of the negotiated codec e.g. "video/vp8", "video/vp9", "audio/opus"
	Reader   transport.TrackRemote
}

// TrackReader wraps a track remote with cleanup functionality
type TrackReader struct {
	track   transport.TrackRemote
	Codec   string // MimeType of the negotiated codec
	onClose func()
}

// NewTrackReader creates a new track reader
func NewTrackReader(track transport.TrackRemote, codec string, onClose func()) *TrackReader {
	return &TrackReader{
		track:   track,
		Codec:   codec,
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

<<<<<<< HEAD
	// Audio recorder for recording tracks
	recorder *recorder.AudioRecorder

	// Recording sessions indexed by trackID
	recordingSessions map[string]*recorder.RecordingSession

	// Jitter buffers for audio tracks (trackID -> jitter buffer)
	jitterBuffers map[string]*jitter.JitterBuffer
=======
	// Jitter buffer for packet loss recovery
	jitterBuffer *jitter.JitterBuffer
>>>>>>> main

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
<<<<<<< HEAD
		recordingSessions: make(map[string]*recorder.RecordingSession),
		jitterBuffers:     make(map[string]*jitter.JitterBuffer),
=======
		jitterBuffer:      jitter.NewJitterBuffer(),
>>>>>>> main
		cleanupTicker:     time.NewTicker(30 * time.Second),
		done:              make(chan struct{}),
	}

	go ps.cleanupLoop()

	return ps
}

// EnableRecording enables audio recording with processing
func (ps *PubSub) EnableRecording(config recorder.RecorderConfig) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.recorder != nil {
		return fmt.Errorf("recording already enabled")
	}

	rec, err := recorder.NewAudioRecorder(config)
	if err != nil {
		return fmt.Errorf("create audio recorder: %w", err)
	}

	ps.recorder = rec
	log.Printf("PubSub: Audio recording enabled with processing")
	return nil
}

// DisableRecording disables audio recording
func (ps *PubSub) DisableRecording() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.recorder == nil {
		return nil
	}

	// Stop all active recordings
	for trackID, session := range ps.recordingSessions {
		if err := ps.recorder.StopRecording(session.ID); err != nil {
			log.Printf("PubSub: Error stopping recording for %s: %v", trackID, err)
		}
		delete(ps.recordingSessions, trackID)
	}

	// Close recorder
	if err := ps.recorder.Close(); err != nil {
		log.Printf("PubSub: Error closing recorder: %v", err)
	}

	ps.recorder = nil
	log.Printf("PubSub: Audio recording disabled")
	return nil
}

// IsRecordingEnabled returns true if recording is enabled
func (ps *PubSub) IsRecordingEnabled() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.recorder != nil
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

	track := reader.track
	trackID := track.Track().ID()
	trackKind := track.Track().Kind().String()

	// Initialize published tracks map for client if needed
	if ps.publishedTracks[clientID] == nil {
		ps.publishedTracks[clientID] = make(map[string]*PubTrack)
	}

	// Create pub track
	pubTrack := &PubTrack{
		ClientID: clientID,
		TrackID:  trackID,
		PeerID:   clientID, // peerID is same as clientID for WebRTC
		Kind:     trackKind,
		Codec:    reader.Codec, // use actual negotiated codec
		Reader:   track,
	}

	ps.publishedTracks[clientID][trackID] = pubTrack
	ps.trackReaders[trackID] = reader

	// Initialize bitrate estimator
	ps.bitrateEstimators[trackID] = NewBitrateEstimator()

	// Log current subscribers count
	subCount := 0
	for _, trackSubs := range ps.subscriptions[clientID] {
		for _, sub := range trackSubs {
			if sub != nil {
				subCount++
			}
		}
	}
	log.Printf("PubSub: [TRACK %s] PUBLISHED - clientID: %s, kind: %s, streamID: %s, current subs: %d",
		trackID, clientID, trackKind, track.Track().StreamID(), subCount)

	// Notify subscribers (within lock to ensure consistency)
	ps.notifyEventSubscribers(PubTrackEvent{
		PubTrack: *pubTrack,
		Type:     TrackEventTypeAdd,
	})

	ps.mu.Unlock()

	// Start forwarding RTP packets with track kind already determined
	// Pass trackKind directly to avoid race conditions
	go ps.forwardTrack(clientID, trackID, reader, trackKind)

	return nil
}

// Terminate terminates all subscriptions and publications for a client
func (ps *PubSub) Terminate(clientID string) {
	ps.mu.Lock()

	// 1. Collect all readers to close to avoid deadlocks with onClose callbacks
	var readersToClose []*TrackReader

	// 2. Unpublish all tracks for this client
	if tracks, ok := ps.publishedTracks[clientID]; ok {
		for trackID, pubTrack := range tracks {
			// Notify subscribers of removal
			ps.notifyEventSubscribers(PubTrackEvent{
				PubTrack: *pubTrack,
				Type:     TrackEventTypeRemove,
			})

			// Collect reader for closing later (outside lock)
			if reader, ok := ps.trackReaders[trackID]; ok {
				readersToClose = append(readersToClose, reader)
				delete(ps.trackReaders, trackID)
			}

			// Remove bitrate estimator
			delete(ps.bitrateEstimators, trackID)
		}
		delete(ps.publishedTracks, clientID)
	}

	// 3. Unsubscribe this client from all tracks it was subscribed to
	// We need to iterate over all publishers and their tracks
	for pubClientID, pubTracksMap := range ps.subscriptions {
		for trackID, subsMap := range pubTracksMap {
			if sub, ok := subsMap[clientID]; ok {
				delete(subsMap, clientID)

				// Notify the publisher that a subscription removed
				if ptMap, ok := ps.publishedTracks[pubClientID]; ok {
					if pubTrack, ok := ptMap[trackID]; ok {
						ps.notifyEventSubscribers(PubTrackEvent{
							PubTrack: *pubTrack,
							Type:     TrackEventTypeUnsub,
							// Note: We don't have the Sub struct here in the event, but we know the ClientID
						})
						_ = sub // Keep compiler happy if sub is unused
					}
				}
			}
		}
	}

	// 4. Clean up event subscriber channel
	if ch, ok := ps.eventSubscribers[clientID]; ok {
		close(ch)
		delete(ps.eventSubscribers, clientID)
	}

	ps.mu.Unlock()

	// 5. Close readers outside the lock to avoid deadlocks!
	for _, reader := range readersToClose {
		reader.Close()
	}

	log.Printf("PubSub: Client terminated - all tracks and subscriptions removed: %s", clientID)
}

// Unpub unpublishes a track
func (ps *PubSub) Unpub(clientID, trackID string) {
	ps.mu.Lock()
	reader := ps.unpubInternal(clientID, trackID)
	ps.mu.Unlock()

	// Close reader outside the lock
	if reader != nil {
		reader.Close()
	}
}

// unpubInternal performs the unpublish logic without locking ps.mu
func (ps *PubSub) unpubInternal(clientID, trackID string) *TrackReader {
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

	// Remove and return track reader for closing outside lock
	var reader *TrackReader
	if r, ok := ps.trackReaders[trackID]; ok {
		reader = r
		delete(ps.trackReaders, trackID)
	}

	// Remove bitrate estimator
	delete(ps.bitrateEstimators, trackID)

	// Remove all subscriptions for this track
	delete(ps.subscriptions[clientID], trackID)

	log.Printf("PubSub: Track unpublished - clientID: %s, trackID: %s", clientID, trackID)
	return reader
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

	log.Printf("PubSub: [TRACK %s] SUBSCRIBED - pubClientID: %s, subClientID: %s, kind: %s, total subs: %d",
		trackID, pubClientID, subClientID, pubTrack.Kind, len(ps.subscriptions[pubClientID][trackID]))

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
<<<<<<< HEAD
func (ps *PubSub) forwardTrack(clientID, trackID string, reader *TrackReader, trackKind string) {
	ps.forwardDirect(clientID, trackID, reader, trackKind == "video")
}

// forwardAudioWithJitterBuffer forwards audio with jitter buffering (kept for reference/future use)
func (ps *PubSub) forwardAudioWithJitterBuffer(clientID, trackID string, reader *TrackReader, jb *jitter.JitterBuffer) {
	log.Printf("PubSub: Starting jitter-buffered forwarding for audio track %s", trackID)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for {
			packet, err := reader.ReadRTP()
			if err != nil {
				return
			}
			jb.Push(packet, time.Now())
		}
	}()
	for {
		select {
		case <-doneCh:
			return
		case <-ticker.C:
			packet := jb.Pop(time.Now())
			if packet == nil {
				continue
			}
			ps.mu.RLock()
			subs, ok := ps.subscriptions[clientID][trackID]
			ps.mu.RUnlock()
			if !ok {
				continue
			}
			for _, sub := range subs {
				_ = sub.Writer.WriteRTP(packet)
			}
		}
	}
}

// forwardDirect forwards packets directly without jitter buffering
func (ps *PubSub) forwardDirect(clientID, trackID string, reader *TrackReader, isVideoTrack bool) {
	trackType := "audio"
	if isVideoTrack {
		trackType = "video"
	}
	log.Printf("PubSub: [TRACK %s] Starting direct forwarding for %s track from client %s", trackID, trackType, clientID)

	packetCount := 0
	lastLogTime := time.Now()
=======
func (ps *PubSub) forwardTrack(clientID, trackID string, reader *TrackReader) {
	// Read first packet to get the original SSRC
	firstPacket, err := reader.ReadRTP()
	if err != nil {
		log.Printf("PubSub: Error reading first RTP from track %s: %v", trackID, err)
		return
	}
>>>>>>> main

	// Use the original SSRC from the first packet
	// DO NOT rewrite SSRC - this breaks video decoding!
	originalSSRC := firstPacket.SSRC
	log.Printf("PubSub: Starting track forwarding for %s with SSRC %d", trackID, originalSSRC)

	// Get or create jitter buffer for this track using ORIGINAL SSRC
	jb := ps.jitterBuffer.GetOrCreateBuffer(originalSSRC)

	// Process first packet
	ps.processAndForwardPacket(clientID, trackID, firstPacket, jb, originalSSRC)

	// Continue with remaining packets
	for {
		packet, err := reader.ReadRTP()
		if err != nil {
<<<<<<< HEAD
			if err == io.EOF {
				log.Printf("PubSub: [TRACK %s] Track %s closed (EOF)", trackID, trackType)
			} else {
				log.Printf("PubSub: [TRACK %s] Error reading RTP: %v", trackID, err)
			}
			return
		}

		packetCount++
=======
			log.Printf("PubSub: Error reading RTP from track %s: %v", trackID, err)
			// Clean up jitter buffer when track ends
			ps.jitterBuffer.RemoveBuffer(originalSSRC)
			return
		}

		ps.processAndForwardPacket(clientID, trackID, packet, jb, originalSSRC)
	}
}

// processAndForwardPacket processes a single RTP packet and forwards to subscribers
// Following peer-calls pattern: write the same packet to all subscribers
func (ps *PubSub) processAndForwardPacket(clientID, trackID string, packet *rtp.Packet, jb *jitter.Buffer, originalSSRC uint32) {
	// Push packet to jitter buffer for packet loss detection
	// The jitter buffer tracks sequence numbers and can generate NACKs
	nackPacket := jb.Push(packet)

	// If jitter buffer detected missing packets, we need to send a NACK
	// to the source to request retransmission
	if nackPacket != nil {
		// Get the source transport to send NACK
>>>>>>> main
		ps.mu.RLock()
		sourceTransport, ok := ps.publishedTracks[clientID]
		ps.mu.RUnlock()

		if ok && sourceTransport != nil {
			// We need to send the NACK to the source
			// This will be handled by the peer_manager which has access to transports
			// For now, log it - the peer_manager's RTCP handler will handle NACKs from subscribers
			if nack, ok := nackPacket.(*rtcp.TransportLayerNack); ok {
				log.Printf("PubSub: Jitter buffer requesting NACK for %d packets on track %s", len(nack.Nacks), trackID)
			}
		}
	}

	// Get subscribers
	ps.mu.RLock()
	tracksMap, ok := ps.subscriptions[clientID]
	if !ok {
		ps.mu.RUnlock()
		return
	}

	subs, ok := tracksMap[trackID]
	ps.mu.RUnlock()

	if !ok || len(subs) == 0 {
		return
	}

	// Ensure packet has correct SSRC (should already be set from original)
	packet.Header.SSRC = originalSSRC

	// Marshal the packet once and clone per-subscriber to avoid sharing the same
	// packet instance across different writers (some writers may mutate the packet).
	raw, err := packet.Marshal()
	if err != nil {
		log.Printf("PubSub: Error marshaling RTP packet for track %s: %v", trackID, err)
		return
	}

	for _, sub := range subs {
		// Unmarshal into a fresh packet instance for each subscriber
		cloned := &rtp.Packet{}
		if err := cloned.Unmarshal(raw); err != nil {
			log.Printf("PubSub: Error unmarshaling RTP packet for subscriber %s: %v", sub.ClientID, err)
			continue
		}

<<<<<<< HEAD
		for _, sub := range subs {
			if err := sub.Writer.WriteRTP(packet); err != nil {
				// Throttle logging
				if packetCount%1000 == 0 {
					log.Printf("PubSub: [TRACK %s] Error writing RTP to sub %s: %v", trackID, sub.ClientID, err)
				}
			}
		}

		if isVideoTrack && time.Since(lastLogTime) > 10*time.Second {
			log.Printf("PubSub: [TRACK %s] VIDEO STATS: forwarded %d total packets to %d subscribers",
				trackID, packetCount, len(subs))
			lastLogTime = time.Now()
		}
=======
		// Ensure cloned packet has the original SSRC
		cloned.Header.SSRC = originalSSRC

		if err := sub.Writer.WriteRTP(cloned); err != nil {
			log.Printf("PubSub: Error writing RTP to subscriber %s: %v", sub.ClientID, err)
		}
>>>>>>> main
	}
}

// AutoSubscribe automatically subscribes all existing subscribers to a new track
func (ps *PubSub) AutoSubscribe(pubClientID, trackID string, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for subClientID := range ps.eventSubscribers {
		if subClientID == pubClientID {
			continue
		}

		if ps.subscriptions[pubClientID] == nil {
			ps.subscriptions[pubClientID] = make(map[string]map[string]*Sub)
		}
		if ps.subscriptions[pubClientID][trackID] == nil {
			ps.subscriptions[pubClientID][trackID] = make(map[string]*Sub)
		}

		ps.subscriptions[pubClientID][trackID][subClientID] = &Sub{
			ClientID:   subClientID,
			Writer:     writer,
			RTCPReader: rtcpReader,
		}

		log.Printf("PubSub: Auto-subscribed %s to track %s from %s", subClientID, trackID, pubClientID)
	}

	return nil
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

// GetActiveRecordings returns list of active recording track IDs
func (ps *PubSub) GetActiveRecordings() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	ids := make([]string, 0, len(ps.recordingSessions))
	for id := range ps.recordingSessions {
		ids = append(ids, id)
	}
	return ids
}

// GetJitterStats returns jitter buffer statistics
func (ps *PubSub) GetJitterStats() map[uint32]struct {
	Received uint64
	Lost     uint64
} {
	return ps.jitterBuffer.Stats()
}

// Close closes the PubSub
func (ps *PubSub) Close() {
	// Stop all recordings
	if ps.recorder != nil {
		ps.DisableRecording()
	}

	close(ps.done)
	ps.cleanupTicker.Stop()
	ps.jitterBuffer.Clear()
}
