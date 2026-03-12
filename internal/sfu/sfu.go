package sfu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"Remainwith/internal/sfu/pubsub"
	"Remainwith/internal/transport"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	sfuWriteWait  = 10 * time.Second
	sfuPongWait   = 30 * time.Second
	sfuPingPeriod = 10 * time.Second
)

// SFU represents the Selective Forwarding Unit server
type SFU struct {
	tracksManager *TracksManager
	upgrader      websocket.Upgrader
	mu            sync.RWMutex
	clients       map[string]*Client
}

// Client represents a connected SFU client
type Client struct {
	ID        string
	RoomID    string
	Conn      *websocket.Conn
	Transport *transport.WebRTCTransport
	mu        sync.Mutex // Protects renegotiation
	// Pending track subscriptions waiting for answer
	pendingSubs map[string]*pendingSub
	// Active video subscriptions keyed by logical source key.
	activeVideoSubs map[string]*activeVideoSub
	// Track events queue for initial connection
	initialTrackEvents []pubsub.PubTrackEvent
	// Track events queued while a renegotiation offer is already in flight.
	queuedTrackEvents []pubsub.PubTrackEvent
	// Flag to indicate if initial connection is established
	initialConnected bool
	// True when the SFU has sent an offer and is waiting for the client's answer.
	awaitingAnswer bool
	// Pending ICE candidates waiting for remote description
	pendingCandidates []*webrtc.ICECandidate
	// Channel to signal when local description is set (for ICE candidates)
	descriptionSent     chan struct{}
	descriptionSentOnce sync.Once
	// Phase 10: ICE restart state
	iceRestartPending bool
	iceRestartCount   int
	// Phase 9: Connection monitoring
	lastActivity    time.Time
	connectionState webrtc.PeerConnectionState
	// Write channel for serializing WebSocket writes
	writeCh    chan interface{}
	closed     chan struct{}
	closedOnce sync.Once
	// Preferred incoming video quality for this client.
	receiveQuality string
}

// pendingSub represents a pending track subscription
type pendingSub struct {
	subscriptionKey string
	sourceKey       string
	pubTrack        pubsub.PubTrack
	trackLocal      *trackLocalImpl
	rtcpReader      *rtcpReaderImpl
	rtpSender       *webrtc.RTPSender
}

type activeVideoSub struct {
	subscriptionKey string
	sourceKey       string
	pubTrack        pubsub.PubTrack
	trackLocal      *trackLocalImpl
	rtcpReader      *rtcpReaderImpl
	rtpSender       *webrtc.RTPSender
}

// SignalMessage represents a signaling message
type SignalMessage struct {
	Type        string                 `json:"type"`
	ClientID    string                 `json:"client_id,omitempty"`
	RoomID      string                 `json:"room_id,omitempty"`
	Offer       string                 `json:"offer,omitempty"`
	Answer      string                 `json:"answer,omitempty"`
	Candidate   map[string]interface{} `json:"candidate,omitempty"`
	TrackID     string                 `json:"track_id,omitempty"`
	PubClientID string                 `json:"pub_client_id,omitempty"`
	Quality     string                 `json:"quality,omitempty"`
	// Ready indicates client is ready to receive offers
	Ready bool `json:"ready,omitempty"`
	// Phase 10: ICE restart flag
	ICERestart bool `json:"ice_restart,omitempty"`
}

// PubTrackMessage represents a track subscription message
type PubTrackMessage struct {
	Type        string `json:"type"`
	PubClientID string `json:"pub_client_id"`
	TrackID     string `json:"track_id"`
	Kind        string `json:"kind"`
}

// NewSFU creates a new SFU server
func NewSFU() *SFU {
	return &SFU{
		tracksManager: NewTracksManager(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
		clients: make(map[string]*Client),
	}
}

func sfuSessionKey(roomID, clientID string) string {
	return roomID + ":" + clientID
}

// New creates a new SFU server (compatibility with existing code)
func New(ctx context.Context, opts interface{}) *SFU {
	return NewSFU()
}

// DefaultOptions returns default options (compatibility with existing code)
func DefaultOptions() interface{} {
	return nil
}

// ServeHTTP handles WebSocket connections
func (s *SFU) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	roomID := r.URL.Query().Get("room_id")

	if clientID == "" || roomID == "" {
		http.Error(w, "client_id and room_id required", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("SFU: Failed to upgrade connection: %v", err)
		return
	}

	if err := conn.SetReadDeadline(time.Now().Add(sfuPongWait)); err != nil {
		log.Printf("SFU: Failed to set initial read deadline: %v", err)
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(sfuPongWait))
	})

	// Create WebRTC transport with STUN and TURN servers
	// Phase 9: Enhanced ICE configuration for connection stability
	iceServers := []webrtc.ICEServer{
		// Multiple Google STUN servers for redundancy
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		{URLs: []string{"stun:stun1.l.google.com:19302"}},
		{URLs: []string{"stun:stun2.l.google.com:19302"}},
		{URLs: []string{"stun:stun3.l.google.com:19302"}},
		{URLs: []string{"stun:stun4.l.google.com:19302"}},
		// Cloudflare STUN server
		{URLs: []string{"stun:stun.cloudflare.com:3478"}},
		// Public TURN servers for testing (replace with your own in production)
		{
			URLs:       []string{"turn:openrelay.metered.ca:80"},
			Username:   "openrelayproject",
			Credential: "openrelayproject",
		},
		{
			URLs:       []string{"turn:openrelay.metered.ca:443"},
			Username:   "openrelayproject",
			Credential: "openrelayproject",
		},
		{
			URLs:       []string{"turn:openrelay.metered.ca:443?transport=tcp"},
			Username:   "openrelayproject",
			Credential: "openrelayproject",
		},
	}

	webrtcTransport, err := transport.NewWebRTCTransport(clientID, roomID, iceServers)
	if err != nil {
		log.Printf("SFU: Failed to create WebRTC transport: %v", err)
		conn.Close()
		return
	}

	// NOTE: We no longer create upfront transceivers.
	// The client will send an offer with its tracks, and we'll create matching transceivers.
	// This ensures proper transceiver alignment between client and SFU.
	pc := webrtcTransport.GetPeerConnection()
	if pc == nil {
		log.Printf("SFU: ERROR - PeerConnection is nil for client %s", clientID)
	}

	client := &Client{

		ID:                 clientID,
		RoomID:             roomID,
		Conn:               conn,
		Transport:          webrtcTransport,
		pendingSubs:        make(map[string]*pendingSub),
		activeVideoSubs:    make(map[string]*activeVideoSub),
		initialTrackEvents: make([]pubsub.PubTrackEvent, 0),
		queuedTrackEvents:  make([]pubsub.PubTrackEvent, 0),
		initialConnected:   false,
		pendingCandidates:  make([]*webrtc.ICECandidate, 0),
		descriptionSent:    make(chan struct{}),
		lastActivity:       time.Now(),
		connectionState:    webrtc.PeerConnectionStateNew,
		writeCh:            make(chan interface{}, 256),
		closed:             make(chan struct{}),
		receiveQuality:     "auto",
	}

	// Start dedicated write goroutine to serialize WebSocket writes
	go s.writePump(client)

	sessionKey := sfuSessionKey(roomID, clientID)

	s.mu.Lock()
	if existing, ok := s.clients[sessionKey]; ok {
		log.Printf("SFU: Replacing existing client session for %s in room %s", clientID, roomID)
		if existing.Conn != nil {
			_ = existing.Conn.Close()
		}
		if existing.Transport != nil {
			_ = existing.Transport.Close()
		}
	}
	s.clients[sessionKey] = client
	s.mu.Unlock()

	log.Printf("SFU: Client %s joined room %s", clientID, roomID)

	// Add transport to tracks manager
	pubTrackEventsCh, err := s.tracksManager.Add(roomID, webrtcTransport)
	if err != nil {
		log.Printf("SFU: Failed to add transport to tracks manager: %v", err)
		conn.Close()
		webrtcTransport.Close()
		return
	}

	// Handle pub track events
	go s.handlePubTrackEvents(client, pubTrackEventsCh)

	// Handle WebRTC signals
	go s.handleSignals(client)

	// Ensure abrupt transport shutdowns always evict the client session.
	go func() {
		<-webrtcTransport.Done()
		s.shutdownClient(client)
	}()

	// Phase 9: Start connection monitoring
	go s.monitorConnection(client)

	// Handle client messages (this will wait for ready signal before sending offer)
	s.handleClientMessages(client)
}

// writePump serializes WebSocket writes through a channel
func (s *SFU) writePump(client *Client) {
	for {
		select {
		case <-client.closed:
			return
		case msg := <-client.writeCh:
			switch m := msg.(type) {
			case string:
				// Ping message
				if err := client.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(sfuWriteWait)); err != nil {
					log.Printf("SFU: Error sending ping to client %s: %v", client.ID, err)
					s.shutdownClient(client)
					return
				}
			default:
				// JSON message
				if err := client.Conn.SetWriteDeadline(time.Now().Add(sfuWriteWait)); err != nil {
					log.Printf("SFU: Error setting write deadline for client %s: %v", client.ID, err)
					s.shutdownClient(client)
					return
				}
				if err := client.Conn.WriteJSON(m); err != nil {
					log.Printf("SFU: Error writing to client %s: %v", client.ID, err)
					s.shutdownClient(client)
					return
				}
			}
		}
	}
}

func (s *SFU) enqueueClientMessage(client *Client, msg interface{}, reason string) bool {
	select {
	case <-client.closed:
		return false
	case <-client.Transport.Done():
		return false
	default:
	}

	select {
	case <-client.closed:
		return false
	case <-client.Transport.Done():
		return false
	case client.writeCh <- msg:
		return true
	default:
		log.Printf("SFU: Failed to queue %s for client %s: write channel full", reason, client.ID)
		return false
	}
}

func (s *SFU) shutdownClient(client *Client) {
	client.closedOnce.Do(func() {
		sessionKey := sfuSessionKey(client.RoomID, client.ID)

		s.mu.Lock()
		if current, ok := s.clients[sessionKey]; ok && current == client {
			delete(s.clients, sessionKey)
		}
		s.mu.Unlock()

		close(client.closed)
		_ = client.Conn.Close()
		if err := client.Transport.Close(); err != nil {
			log.Printf("SFU: Error closing transport for client %s: %v", client.ID, err)
		}
		log.Printf("SFU: Client %s disconnected and cleanup completed", client.ID)
	})
}

// Phase 9: monitorConnection monitors the peer connection state and handles recovery
func (s *SFU) monitorConnection(client *Client) {
	pc := client.Transport.GetPeerConnection()
	if pc == nil {
		return
	}

	// Monitor connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		client.mu.Lock()
		client.connectionState = state
		client.lastActivity = time.Now()
		client.mu.Unlock()

		log.Printf("SFU: Connection state changed to %s for client %s", state.String(), client.ID)

		switch state {
		case webrtc.PeerConnectionStateFailed:
			log.Printf("SFU: Connection failed for client %s, attempting ICE restart", client.ID)
			s.handleConnectionFailure(client)
		case webrtc.PeerConnectionStateDisconnected:
			log.Printf("SFU: Connection disconnected for client %s, monitoring for recovery", client.ID)
			// Give it some time to recover naturally
			go s.waitForRecovery(client)
		case webrtc.PeerConnectionStateConnected:
			log.Printf("SFU: Connection established for client %s", client.ID)
			client.mu.Lock()
			client.iceRestartCount = 0 // Reset restart counter on successful connection
			client.mu.Unlock()
		}
	})

	// Phase 9: Periodic keepalive check
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			client.mu.Lock()
			inactive := time.Since(client.lastActivity) > 60*time.Second
			state := client.connectionState

			if inactive && state != webrtc.PeerConnectionStateClosed {
				log.Printf("SFU: Client %s inactive for 60 seconds, checking health", client.ID)
				// Send a keepalive ping via WebSocket
				if !s.enqueueClientMessage(client, "ping", "keepalive ping") {
					client.mu.Unlock()
					return
				}
			}
			client.mu.Unlock()
		case <-client.Transport.Done():
			return
		}
	}

}

// Phase 10: handleConnectionFailure handles connection failures with ICE restart
func (s *SFU) handleConnectionFailure(client *Client) {
	client.mu.Lock()
	if client.iceRestartPending {
		client.mu.Unlock()
		log.Printf("SFU: ICE restart already pending for client %s", client.ID)
		return
	}

	// Limit restart attempts
	if client.iceRestartCount >= 3 {
		client.mu.Unlock()
		log.Printf("SFU: Max ICE restart attempts reached for client %s, closing connection", client.ID)
		s.closeClient(client)
		return
	}

	client.iceRestartPending = true
	client.iceRestartCount++
	client.mu.Unlock()

	log.Printf("SFU: Initiating ICE restart for client %s (attempt %d)", client.ID, client.iceRestartCount)

	// Create new offer with ICE restart
	offer, err := client.Transport.CreateOffer()
	if err != nil {
		log.Printf("SFU: Failed to create ICE restart offer for client %s: %v", client.ID, err)
		client.mu.Lock()
		client.iceRestartPending = false
		client.mu.Unlock()
		return
	}

	// Send ICE restart offer to client
	msg := SignalMessage{
		Type:       "offer",
		Offer:      offer.SDP,
		ICERestart: true,
	}

	select {
	case <-client.closed:
		log.Printf("SFU: Client %s closed before ICE restart offer could be sent", client.ID)
	case <-client.Transport.Done():
		log.Printf("SFU: Transport already closed for client %s before ICE restart offer could be sent", client.ID)
	case client.writeCh <- msg:
		log.Printf("SFU: Sent ICE restart offer to client %s", client.ID)
	default:
		log.Printf("SFU: Failed to send ICE restart offer to client %s: write channel full", client.ID)
		client.mu.Lock()
		client.iceRestartPending = false
		client.mu.Unlock()
		return
	}
}

// Phase 10: waitForRecovery waits for natural recovery before triggering ICE restart
func (s *SFU) waitForRecovery(client *Client) {
	// Wait 5 seconds for natural recovery
	time.Sleep(5 * time.Second)

	client.mu.Lock()
	state := client.connectionState
	client.mu.Unlock()

	if state == webrtc.PeerConnectionStateDisconnected || state == webrtc.PeerConnectionStateFailed {
		log.Printf("SFU: No natural recovery for client %s, triggering ICE restart", client.ID)
		s.handleConnectionFailure(client)
	}
}

// closeClient closes a client connection cleanly
func (s *SFU) closeClient(client *Client) {
	s.shutdownClient(client)
	log.Printf("SFU: Closed client %s connection", client.ID)
}

// handlePubTrackEvents handles track publication events
func (s *SFU) handlePubTrackEvents(client *Client, eventsCh <-chan pubsub.PubTrackEvent) {
	log.Printf("SFU: Starting pub track event handler for client %s", client.ID)
	for event := range eventsCh {
		// Send event to client via WebSocket
		msg := PubTrackMessage{
			Type:        string(event.Type),
			PubClientID: event.PubTrack.ClientID,
			TrackID:     event.PubTrack.TrackID,
			Kind:        event.PubTrack.Kind,
		}

		select {
		case <-client.closed:
			return
		case <-client.Transport.Done():
			return
		case client.writeCh <- msg:
		default:
			log.Printf("SFU: WARNING - dropping pub track event for client %s: write channel full", client.ID)
		}

		// If this is a new track from another peer, add it to this client's peer connection
		if event.Type == pubsub.TrackEventTypeAdd && event.PubTrack.ClientID != client.ID {
			client.mu.Lock()
			quality := client.receiveQuality
			client.mu.Unlock()
			selectedTrack, ok := s.selectTrackForQuality(client.RoomID, quality, event.PubTrack)
			if !ok {
				continue
			}

			subKey := subscriptionKeyForTrack(selectedTrack)

			log.Printf("SFU: Track ADD event - track %s (kind=%s) from %s for client %s, initialConnected=%v",
				selectedTrack.TrackID, selectedTrack.Kind, selectedTrack.ClientID, client.ID, client.initialConnected)

			client.mu.Lock()
			if pending, exists := client.pendingSubs[subKey]; exists && pending.pubTrack.TrackID != selectedTrack.TrackID {
				pending.pubTrack = selectedTrack
				client.mu.Unlock()
				continue
			}
			if active, exists := client.activeVideoSubs[subKey]; exists {
				currentTrackID := active.pubTrack.TrackID
				client.mu.Unlock()
				if currentTrackID != selectedTrack.TrackID {
					s.switchActiveVideoLayer(client, active, selectedTrack)
				}
				continue
			}

			// If initial connection not yet established, queue the track event
			if !client.initialConnected {
				log.Printf("SFU: QUEUING track %s from %s for client %s (initial connection pending)",
					selectedTrack.TrackID, selectedTrack.ClientID, client.ID)
				client.initialTrackEvents = append(client.initialTrackEvents, pubsub.PubTrackEvent{
					PubTrack: selectedTrack,
					Type:     event.Type,
				})
				client.mu.Unlock()
				continue
			}

			pc := client.Transport.GetPeerConnection()
			if client.awaitingAnswer || (pc != nil && pc.SignalingState() != webrtc.SignalingStateStable) {
				log.Printf("SFU: QUEUING track %s from %s for client %s (renegotiation in progress, signaling=%v awaitingAnswer=%v)",
					selectedTrack.TrackID, selectedTrack.ClientID, client.ID, pc.SignalingState(), client.awaitingAnswer)
				client.queuedTrackEvents = append(client.queuedTrackEvents, pubsub.PubTrackEvent{
					PubTrack: selectedTrack,
					Type:     event.Type,
				})
				client.mu.Unlock()
				continue
			}
			client.mu.Unlock()

			log.Printf("SFU: IMMEDIATELY adding track %s from %s to client %s",
				selectedTrack.TrackID, selectedTrack.ClientID, client.ID)
			go s.addTrackToClient(client, selectedTrack)
		} else if event.Type == pubsub.TrackEventTypeAdd {
			log.Printf("SFU: Ignoring own track event - track %s from self (%s)",
				event.PubTrack.TrackID, client.ID)
		}
	}
	log.Printf("SFU: Pub track event handler ENDED for client %s", client.ID)
}

// processQueuedTrackEvents processes any track events that were queued during initial connection
func (s *SFU) processQueuedTrackEvents(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.initialTrackEvents) == 0 {
		return
	}

	log.Printf("SFU: BATCH processing %d queued track events for client %s", len(client.initialTrackEvents), client.ID)

	selectedEvents := make(map[string]pubsub.PubTrackEvent)
	for _, event := range client.initialTrackEvents {
		selected, ok := s.selectTrackForQuality(client.RoomID, client.receiveQuality, event.PubTrack)
		if !ok {
			continue
		}
		selectedEvents[subscriptionKeyForTrack(selected)] = pubsub.PubTrackEvent{
			PubTrack: selected,
			Type:     event.Type,
		}
	}

	var addedAny bool
	for _, event := range selectedEvents {
		log.Printf("SFU: Batching queued track %s from %s for client %s",
			event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID)

		// Add track without triggering offer yet
		if _, err := s.addTrackInternal(client, event.PubTrack); err == nil {
			addedAny = true
		}
	}

	// Clear the queue
	client.initialTrackEvents = client.initialTrackEvents[:0]

	// Send a single offer for all added tracks
	if addedAny {
		log.Printf("SFU: Creating single batch renegotiation offer for client %s", client.ID)
		offer, err := client.Transport.CreateOffer()
		if err != nil {
			log.Printf("SFU: [BATCH] ERROR creating offer: %v", err)
			return
		}

		msg := SignalMessage{
			Type:  "offer",
			Offer: offer.SDP,
		}

		select {
		case <-client.closed:
			return
		case <-client.Transport.Done():
			return
		case client.writeCh <- msg:
			client.awaitingAnswer = true
			log.Printf("SFU: Sent batch renegotiation offer to client %s", client.ID)
		default:
			log.Printf("SFU: [BATCH] Error sending offer: write channel full")
		}
	}
}

func (s *SFU) processQueuedRenegotiationEvents(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.queuedTrackEvents) == 0 {
		return
	}

	pc := client.Transport.GetPeerConnection()
	if pc == nil {
		return
	}

	if client.awaitingAnswer || pc.SignalingState() != webrtc.SignalingStateStable {
		log.Printf("SFU: Deferring queued renegotiation events for client %s (awaitingAnswer=%v signaling=%v)",
			client.ID, client.awaitingAnswer, pc.SignalingState())
		return
	}

	log.Printf("SFU: Processing %d queued renegotiation events for client %s", len(client.queuedTrackEvents), client.ID)

	selectedEvents := make(map[string]pubsub.PubTrackEvent)
	for _, event := range client.queuedTrackEvents {
		selected, ok := s.selectTrackForQuality(client.RoomID, client.receiveQuality, event.PubTrack)
		if !ok {
			continue
		}
		selectedEvents[subscriptionKeyForTrack(selected)] = pubsub.PubTrackEvent{
			PubTrack: selected,
			Type:     event.Type,
		}
	}

	client.queuedTrackEvents = client.queuedTrackEvents[:0]

	var addedAny bool
	for _, event := range selectedEvents {
		if event.Type != pubsub.TrackEventTypeAdd {
			continue
		}

		subKey := subscriptionKeyForTrack(event.PubTrack)
		if _, exists := client.pendingSubs[subKey]; exists {
			continue
		}
		if active, exists := client.activeVideoSubs[subKey]; exists {
			if active.pubTrack.TrackID != event.PubTrack.TrackID {
				go s.switchActiveVideoLayer(client, active, event.PubTrack)
			}
			continue
		}

		log.Printf("SFU: Batch-adding queued track %s from %s for client %s",
			event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID)
		if _, err := s.addTrackInternal(client, event.PubTrack); err == nil {
			addedAny = true
		}
	}

	if !addedAny {
		return
	}

	log.Printf("SFU: Creating queued renegotiation offer for client %s", client.ID)
	offer, err := client.Transport.CreateOffer()
	if err != nil {
		log.Printf("SFU: [QUEUED BATCH] ERROR creating offer: %v", err)
		return
	}

	msg := SignalMessage{
		Type:  "offer",
		Offer: offer.SDP,
	}

	select {
	case <-client.closed:
		return
	case <-client.Transport.Done():
		return
	case client.writeCh <- msg:
		client.awaitingAnswer = true
		log.Printf("SFU: Sent queued renegotiation offer to client %s", client.ID)
	default:
		log.Printf("SFU: [QUEUED BATCH] Error sending offer: write channel full")
	}
}

func subscriptionKeyForTrack(track pubsub.PubTrack) string {
	if track.Kind == "video" && track.BaseTrackID != "" {
		return fmt.Sprintf("%s:%s:%s", track.ClientID, track.BaseTrackID, track.Kind)
	}
	return fmt.Sprintf("%s:%s:%s", track.ClientID, track.TrackID, track.Kind)
}

func ridRank(rid string) int {
	switch rid {
	case "q", "low":
		return 1
	case "h", "mid", "medium":
		return 2
	case "f", "high":
		return 3
	default:
		return 0
	}
}

func targetRankForQuality(quality string) int {
	switch quality {
	case "360p":
		return 1
	case "480p", "720p":
		return 2
	case "1080p":
		return 3
	default:
		return 3
	}
}

func pickPreferredTrack(tracks []pubsub.PubTrack, quality string) (pubsub.PubTrack, bool) {
	if len(tracks) == 0 {
		return pubsub.PubTrack{}, false
	}

	best := tracks[0]
	bestScore := -1
	target := targetRankForQuality(quality)

	for _, track := range tracks {
		if track.Kind != "video" {
			return track, true
		}

		rank := ridRank(track.RID)
		score := 0
		if quality == "auto" {
			score = rank
		} else {
			diff := target - rank
			if diff < 0 {
				diff = -diff
			}
			score = 100 - diff*10 + rank
		}

		if score > bestScore {
			best = track
			bestScore = score
		}
	}

	return best, true
}

func (s *SFU) selectTrackForQuality(roomID, quality string, candidate pubsub.PubTrack) (pubsub.PubTrack, bool) {
	if candidate.Kind != "video" || candidate.BaseTrackID == "" {
		return candidate, true
	}

	allTracks := s.tracksManager.GetTracksForPublisher(roomID, candidate.ClientID)
	matching := make([]pubsub.PubTrack, 0)
	for _, track := range allTracks {
		if track.Kind == "video" && track.BaseTrackID == candidate.BaseTrackID {
			matching = append(matching, track)
		}
	}

	return pickPreferredTrack(matching, quality)
}

// addTrackInternal performs the track setup without triggering a renegotiation offer.
// Must be called with client.mu locked.
func (s *SFU) addTrackInternal(client *Client, pubTrack pubsub.PubTrack) (*webrtc.RTPSender, error) {
	log.Printf("SFU: [ADD TRACK INTERNAL] track %s (kind=%s) from %s to client %s", pubTrack.TrackID, pubTrack.Kind, pubTrack.ClientID, client.ID)
	subscriptionKey := subscriptionKeyForTrack(pubTrack)

	// Get codec capability based on track kind
	var codecCapability webrtc.RTPCodecCapability
	switch pubTrack.Kind {
	case "video":
		codecCapability = webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8,
			RTCPFeedback: []webrtc.RTCPFeedback{
				{Type: "nack"},
				{Type: "nack", Parameter: "pli"},
				{Type: "goog-remb"},
				{Type: "transport-cc"},
			},
		}
	case "audio":
		codecCapability = webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus,
		}
	default:
		return nil, fmt.Errorf("unknown track kind: %v", pubTrack.Kind)
	}

	forwardTrackID := pubTrack.TrackID
	if pubTrack.Kind == "video" && pubTrack.BaseTrackID != "" {
		forwardTrackID = pubTrack.BaseTrackID
	}

	streamID := fmt.Sprintf("pub-%s-%s", pubTrack.ClientID, forwardTrackID)
	trackToForward, err := webrtc.NewTrackLocalStaticRTP(
		codecCapability,
		forwardTrackID,
		streamID,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating forward track: %w", err)
	}

	transceiver, err := client.Transport.GetPeerConnection().AddTransceiverFromTrack(trackToForward, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	if err != nil {
		return nil, fmt.Errorf("error adding transceiver: %w", err)
	}

	rtpSender := transceiver.Sender()
	if rtpSender == nil {
		return nil, fmt.Errorf("transceiver has no sender")
	}

	// Create RTCP reader and track local wrapper
	rtcpReader := &rtcpReaderImpl{sender: rtpSender}
	trackLocal := &trackLocalImpl{track: trackToForward}

	// Store as pending subscription
	client.pendingSubs[subscriptionKey] = &pendingSub{
		subscriptionKey: subscriptionKey,
		sourceKey:       subscriptionKey,
		pubTrack:        pubTrack,
		trackLocal:      trackLocal,
		rtcpReader:      rtcpReader,
		rtpSender:       rtpSender,
	}

	// Route RTCP feedback for this subscribed track back to its publisher.
	go s.processRTCP(client.RoomID, rtpSender, pubTrack)

	return rtpSender, nil
}

// addTrackToClient adds a published track to a client's peer connection and triggers renegotiation
func (s *SFU) addTrackToClient(client *Client, pubTrack pubsub.PubTrack) {
	client.mu.Lock()
	defer client.mu.Unlock()

	log.Printf("SFU: [ADD TRACK] START - track %s (kind=%s) from %s to client %s", pubTrack.TrackID, pubTrack.Kind, pubTrack.ClientID, client.ID)

	rtpSender, err := s.addTrackInternal(client, pubTrack)
	if err != nil {
		log.Printf("SFU: [ADD TRACK] ERROR: %v", err)
		return
	}

	// Renegotiate - create and send offer
	log.Printf("SFU: [ADD TRACK] Creating renegotiation offer for client %s", client.ID)
	offer, err := client.Transport.CreateOffer()
	if err != nil {
		log.Printf("SFU: [ADD TRACK] ERROR creating offer: %v", err)
		// Clean up pending sub
		delete(client.pendingSubs, subscriptionKeyForTrack(pubTrack))
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	}

	// Send offer to client
	msg := SignalMessage{
		Type:  "offer",
		Offer: offer.SDP,
	}

	select {
	case <-client.closed:
		delete(client.pendingSubs, subscriptionKeyForTrack(pubTrack))
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	case <-client.Transport.Done():
		delete(client.pendingSubs, subscriptionKeyForTrack(pubTrack))
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	case client.writeCh <- msg:
		client.awaitingAnswer = true
		log.Printf("SFU: Sent renegotiation offer to client %s for track %s", client.ID, pubTrack.TrackID)
	default:
		log.Printf("SFU: [ADD TRACK] Error sending offer: write channel full")
		// Clean up pending sub
		delete(client.pendingSubs, subscriptionKeyForTrack(pubTrack))
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
	}
}

func (s *SFU) switchActiveVideoLayer(client *Client, active *activeVideoSub, nextTrack pubsub.PubTrack) {
	log.Printf("SFU: Switching active video layer for client %s source %s from %s to %s",
		client.ID, active.sourceKey, active.pubTrack.TrackID, nextTrack.TrackID)

	if active.pubTrack.TrackID == nextTrack.TrackID {
		return
	}

	oldParams := SubParams{
		PubClientID: active.pubTrack.ClientID,
		RoomID:      client.RoomID,
		TrackID:     active.pubTrack.TrackID,
		SubClientID: client.ID,
	}
	_ = s.tracksManager.Unsub(oldParams)

	newParams := SubParams{
		PubClientID: nextTrack.ClientID,
		RoomID:      client.RoomID,
		TrackID:     nextTrack.TrackID,
		SubClientID: client.ID,
	}

	if err := s.tracksManager.Sub(newParams, active.trackLocal, active.rtcpReader); err != nil {
		log.Printf("SFU: Failed to switch video layer for client %s: %v", client.ID, err)
		_ = s.tracksManager.Sub(oldParams, active.trackLocal, active.rtcpReader)
		return
	}

	client.mu.Lock()
	if stored, ok := client.activeVideoSubs[active.sourceKey]; ok {
		stored.pubTrack = nextTrack
	}
	client.mu.Unlock()

	s.requestKeyframe(client.RoomID, nextTrack.ClientID, nextTrack.TrackID)
}

// completePendingSubscriptions completes pending track subscriptions after answer is received
func (s *SFU) completePendingSubscriptions(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	log.Printf("SFU: [COMPLETE PENDING] START for client %s, pending count=%d", client.ID, len(client.pendingSubs))

	// Log all pending track IDs
	for subKey, pending := range client.pendingSubs {
		log.Printf("SFU: [COMPLETE PENDING] Pending track: %s => %s (kind=%s) from %s",
			subKey, pending.pubTrack.TrackID, pending.pubTrack.Kind, pending.pubTrack.ClientID)
	}

	for subKey, pending := range client.pendingSubs {
		log.Printf("SFU: [COMPLETE PENDING] Processing track %s (kind=%s) from %s for client %s",
			pending.pubTrack.TrackID, pending.pubTrack.Kind, pending.pubTrack.ClientID, client.ID)

		// Subscribe to the track through the tracks manager
		// This connects the PubSub system to forward RTP packets to this writer
		subParams := SubParams{
			PubClientID: pending.pubTrack.ClientID,
			RoomID:      client.RoomID,
			TrackID:     pending.pubTrack.TrackID,
			SubClientID: client.ID,
		}

		if err := s.tracksManager.Sub(subParams, pending.trackLocal, pending.rtcpReader); err != nil {
			log.Printf("SFU: [COMPLETE PENDING] ERROR subscribing to track %s: %v", pending.pubTrack.TrackID, err)
			// Remove the track we added
			client.Transport.GetPeerConnection().RemoveTrack(pending.rtpSender)
		} else {
			log.Printf("SFU: [COMPLETE PENDING] SUCCESS - client %s subscribed to track %s (kind=%s) from %s",
				client.ID, pending.pubTrack.TrackID, pending.pubTrack.Kind, pending.pubTrack.ClientID)

			// CRITICAL: Request a keyframe from the publisher to ensure the subscriber
			// receives a valid video stream immediately
			if pending.pubTrack.Kind == "video" {
				client.activeVideoSubs[pending.sourceKey] = &activeVideoSub{
					subscriptionKey: pending.subscriptionKey,
					sourceKey:       pending.sourceKey,
					pubTrack:        pending.pubTrack,
					trackLocal:      pending.trackLocal,
					rtcpReader:      pending.rtcpReader,
					rtpSender:       pending.rtpSender,
				}
				log.Printf("SFU: [COMPLETE PENDING] Requesting keyframe for video track %s from %s", pending.pubTrack.TrackID, pending.pubTrack.ClientID)
				s.requestKeyframe(client.RoomID, pending.pubTrack.ClientID, pending.pubTrack.TrackID)
			}
		}

		// Remove from pending
		delete(client.pendingSubs, subKey)
	}

	log.Printf("SFU: [COMPLETE PENDING] END for client %s", client.ID)
}

// requestKeyframe sends a PLI to the publisher to request a keyframe
func (s *SFU) requestKeyframe(roomID, pubClientID, trackID string) {
	s.mu.RLock()
	pubClient, ok := s.clients[sfuSessionKey(roomID, pubClientID)]
	s.mu.RUnlock()

	if !ok {
		log.Printf("SFU: Cannot request keyframe - publisher %s not found in room %s", pubClientID, roomID)
		return
	}

	pc := pubClient.Transport.GetPeerConnection()
	if pc == nil {
		log.Printf("SFU: Cannot request keyframe - peer connection nil for %s", pubClientID)
		return
	}

	// Create PLI packet
	pli := &rtcp.PictureLossIndication{
		MediaSSRC: 0, // Will be set by the receiver
	}

	// Write PLI to request keyframe
	if err := pc.WriteRTCP([]rtcp.Packet{pli}); err != nil {
		log.Printf("SFU: Error requesting keyframe from %s: %v", pubClientID, err)
	} else {
		log.Printf("SFU: Successfully requested keyframe from %s for track %s in room %s", pubClientID, trackID, roomID)
	}
}

// processRTCP processes RTCP packets for a sender and forwards PLI to publisher

func (s *SFU) processRTCP(roomID string, rtpSender *webrtc.RTPSender, pubTrack pubsub.PubTrack) {
	rtcpBuf := make([]byte, 1500)
	for {
		n, _, err := rtpSender.Read(rtcpBuf)
		if err != nil {
			return
		}

		// Parse RTCP packets for debugging and forwarding
		packets, err := rtcp.Unmarshal(rtcpBuf[:n])
		if err != nil {
			continue
		}

		for _, packet := range packets {
			switch p := packet.(type) {
			case *rtcp.PictureLossIndication:
				log.Printf("SFU: Received PLI from subscriber for track %s (publisher %s, SSRC %d)",
					pubTrack.TrackID, pubTrack.ClientID, p.MediaSSRC)
				s.requestKeyframe(roomID, pubTrack.ClientID, pubTrack.TrackID)
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				log.Printf("SFU: Received REMB from subscriber: %f bps", p.Bitrate)
			case *rtcp.TransportLayerNack:
				// NACK packets indicate packet loss - normal for video
				log.Printf("SFU: Received NACK from subscriber for SSRC %d", p.MediaSSRC)
			}
		}
	}
}

// forwardPLIToPublisher forwards a PLI packet to the publisher to request a keyframe
func (s *SFU) forwardPLIToPublisher(pli *rtcp.PictureLossIndication) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find the publisher client that owns this SSRC
	for clientID, client := range s.clients {
		pc := client.Transport.GetPeerConnection()
		if pc == nil {
			continue
		}

		// Check if this client has a receiver with matching SSRC
		receivers := pc.GetReceivers()
		for _, receiver := range receivers {
			if receiver.Track() == nil {
				continue
			}

			// Check if this receiver's track matches the PLI SSRC
			if uint32(receiver.Track().SSRC()) == uint32(pli.MediaSSRC) {

				log.Printf("SFU: Forwarding PLI to publisher %s for SSRC %d", clientID, pli.MediaSSRC)

				// Write PLI to the peer connection - this requests a keyframe from the publisher
				if err := pc.WriteRTCP([]rtcp.Packet{pli}); err != nil {
					log.Printf("SFU: Error forwarding PLI to publisher %s: %v", clientID, err)
				} else {
					log.Printf("SFU: Successfully forwarded PLI to publisher %s", clientID)
				}
				return
			}
		}
	}

	log.Printf("SFU: Could not find publisher for SSRC %d to forward PLI", pli.MediaSSRC)
}

func bytesToInt16(data []byte) []int16 {
	if len(data)%2 != 0 {
		data = append(data, 0)
	}
	samples := make([]int16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		samples[i/2] = int16(data[i]) | int16(data[i+1])<<8
	}
	return samples
}

// int16ToBytes converts int16 samples to byte buffer (little-endian)
func int16ToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		data[i*2] = byte(sample)
		data[i*2+1] = byte(sample >> 8)
	}
	return data
}

// rtcpReaderImpl implements RTCPReader for the SFU
type rtcpReaderImpl struct {
	sender *webrtc.RTPSender
}

func (r *rtcpReaderImpl) ReadRTCP() ([]rtcp.Packet, interceptor.Attributes, error) {
	buf := make([]byte, 1500)
	n, _, err := r.sender.Read(buf)
	if err != nil {
		return nil, nil, err
	}

	packets, err := rtcp.Unmarshal(buf[:n])
	if err != nil {
		return nil, nil, err
	}

	return packets, nil, nil
}

// trackLocalImpl implements transport.TrackLocal for the SFU
type trackLocalImpl struct {
	track *webrtc.TrackLocalStaticRTP
}

func (t *trackLocalImpl) Track() transport.Track {
	return &trackImpl{
		id:       t.track.ID(),
		streamID: t.track.StreamID(),
		kind:     t.track.Kind(),
	}
}

func (t *trackLocalImpl) Write(p []byte) (int, error) {
	return t.track.Write(p)
}

func (t *trackLocalImpl) WriteRTP(packet *rtp.Packet) error {
	return t.track.WriteRTP(packet)
}

// trackImpl implements transport.Track
type trackImpl struct {
	id       string
	streamID string
	kind     webrtc.RTPCodecType
}

func (t *trackImpl) ID() string {
	return t.id
}

func (t *trackImpl) StreamID() string {
	return t.streamID
}

func (t *trackImpl) Kind() webrtc.RTPCodecType {
	return t.kind
}

// handleSignals handles WebRTC signaling
func (s *SFU) handleSignals(client *Client) {
	signalCh := client.Transport.SignalChannel()

	for signal := range signalCh {
		msg := SignalMessage{
			Type:     signal.Type,
			ClientID: client.ID,
		}

		if signal.Candidate != nil {
			msg.Candidate = map[string]interface{}{
				"candidate":     signal.Candidate.ToJSON().Candidate,
				"sdpMid":        signal.Candidate.ToJSON().SDPMid,
				"sdpMLineIndex": signal.Candidate.ToJSON().SDPMLineIndex,
			}
		}

		if signal.SDP != "" {
			if signal.Type == "offer" {
				msg.Offer = signal.SDP
			} else if signal.Type == "answer" {
				msg.Answer = signal.SDP
			}
		}

		select {
		case <-client.closed:
			return
		case <-client.Transport.Done():
			return
		case client.writeCh <- msg:
		default:
			log.Printf("SFU: Error sending signal: write channel full")
			return
		}
	}
}

// handleClientMessages handles incoming WebSocket messages
func (s *SFU) handleClientMessages(client *Client) {
	// Channel to signal when to stop
	done := make(chan struct{})

	defer func() {
		close(done)
		s.shutdownClient(client)
	}()

	// Set up ping ticker to keep connection alive
	pingTicker := time.NewTicker(sfuPingPeriod)
	defer pingTicker.Stop()

	// Reconnection tracking
	reconnectAttempts := 0
	maxReconnectAttempts := 3

	// Start ping goroutine - uses writeCh for serialization
	go func() {
		for {
			select {
			case <-pingTicker.C:
				if !s.enqueueClientMessage(client, "ping", "ping") {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Wait for client ready signal before sending initial offer
	clientReady := false

	for {
		var msg SignalMessage
		err := client.Conn.ReadJSON(&msg)
		if err != nil {
			// Check if this is an unexpected close that might benefit from reconnection
			isUnexpectedClose := websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived)

			if isUnexpectedClose && reconnectAttempts < maxReconnectAttempts {
				reconnectAttempts++
				log.Printf("SFU: Abnormal close detected for client %s (attempt %d/%d), signaling for reconnection",
					client.ID, reconnectAttempts, maxReconnectAttempts)

				// Signal the client to reconnect by sending a special message
				// The client should handle this and reconnect
				if s.enqueueClientMessage(client, SignalMessage{Type: "reconnect", ClientID: client.ID}, "reconnect signal") {
					log.Printf("SFU: Sent reconnect signal to client %s", client.ID)
				}
			}

			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Printf("SFU: Unexpected close from client %s: %v", client.ID, err)
			} else {
				log.Printf("SFU: Client %s disconnected: %v", client.ID, err)
			}
			break
		}

		// Update last activity
		client.mu.Lock()
		client.lastActivity = time.Now()
		client.mu.Unlock()

		switch msg.Type {
		case "ready":
			if !clientReady {
				clientReady = true
				log.Printf("SFU: Client %s is ready, waiting for client offer", client.ID)
				// Client will send offer - we don't send initial offer anymore
				// This ensures the client's tracks are properly negotiated
			}

		case "offer":
			s.handleOffer(client, msg)
		case "answer":
			s.handleAnswer(client, msg)
		case "candidate":
			s.handleCandidate(client, msg)
		case "sub_track":
			s.handleSubTrack(client, msg)
		case "unsub_track":
			s.handleUnsubTrack(client, msg)
		case "quality_preference":
			s.handleQualityPreference(client, msg)
		case "leave":
			return
		default:
			log.Printf("SFU: Unknown message type: %s from client %s", msg.Type, client.ID)
		}
	}
}

// handleOffer handles an offer message
func (s *SFU) handleOffer(client *Client, msg SignalMessage) {
	signal := transport.SignalMessage{
		Type: "offer",
		SDP:  msg.Offer,
	}

	if err := client.Transport.Signal(signal); err != nil {
		log.Printf("SFU: Error handling offer: %v", err)
		return
	}

	// After processing the client's offer and sending back an answer,
	// mark the initial connection as established (if not already done).
	// This is needed when the CLIENT sends the initial offer (rather than the SFU),
	// because handleAnswer is only called when the SFU is the offerer.
	client.mu.Lock()
	wasInitial := !client.initialConnected
	if wasInitial {
		client.initialConnected = true
		log.Printf("SFU: Initial connection established (client-initiated offer) for client %s", client.ID)
	}
	client.mu.Unlock()

	// Process any queued track events that arrived before the initial connection
	if wasInitial {
		s.processQueuedTrackEvents(client)
	}
}

// handleAnswer handles an answer message
func (s *SFU) handleAnswer(client *Client, msg SignalMessage) {
	log.Printf("SFU: Received answer from client %s (len=%d)", client.ID, len(msg.Answer))

	// Phase 10: Check if this is an ICE restart answer
	if msg.ICERestart {
		log.Printf("SFU: Received ICE restart answer from client %s", client.ID)
		client.mu.Lock()
		client.iceRestartPending = false
		client.mu.Unlock()
	}

	// Log answer SDP directions for debugging
	if len(msg.Answer) > 0 {
		// Check for sendrecv direction in answer (client should send AND receive)
		hasSendRecv := contains(msg.Answer, "a=sendrecv")
		hasSendOnly := contains(msg.Answer, "a=sendonly")
		hasRecvOnly := contains(msg.Answer, "a=recvonly")
		log.Printf("SFU: Answer SDP directions - sendrecv:%v sendonly:%v recvonly:%v", hasSendRecv, hasSendOnly, hasRecvOnly)

		// Log first 500 chars of answer for inspection
		previewLen := 500
		if len(msg.Answer) < previewLen {
			previewLen = len(msg.Answer)
		}
		log.Printf("SFU: Answer SDP preview: %s", msg.Answer[:previewLen])
	}

	signal := transport.SignalMessage{
		Type: "answer",
		SDP:  msg.Answer,
	}

	if err := client.Transport.Signal(signal); err != nil {
		log.Printf("SFU: Error handling answer from client %s: %v", client.ID, err)
		return
	}

	log.Printf("SFU: Answer processed successfully for client %s", client.ID)

	// Verify transceiver directions after answer is applied
	pc := client.Transport.GetPeerConnection()
	if pc != nil {
		trs := pc.GetTransceivers()
		log.Printf("SFU: After answer, peer connection has %d transceivers", len(trs))
		for i, tr := range trs {
			log.Printf("SFU: After answer - Transceiver[%d] kind=%v direction=%v",
				i, tr.Kind(), tr.Direction())
		}
	}

	// Process any pending ICE candidates now that remote description is set
	s.processPendingCandidates(client)

	// Check if this is the initial connection answer
	client.mu.Lock()
	wasInitial := !client.initialConnected
	if wasInitial {
		client.initialConnected = true
		log.Printf("SFU: Initial connection established for client %s", client.ID)
	}
	client.awaitingAnswer = false
	pendingCount := len(client.pendingSubs)
	client.mu.Unlock()

	// If this was the initial connection, process any queued track events
	// This will add tracks and trigger renegotiation offers
	if wasInitial {
		s.processQueuedTrackEvents(client)
	}

	// Complete any pending track subscriptions now that renegotiation is done
	// Only do this if there are actually pending subscriptions (renegotiation case)
	if pendingCount > 0 {
		s.completePendingSubscriptions(client)
	}

	s.processQueuedRenegotiationEvents(client)

}

// handleCandidate handles an ICE candidate message
func (s *SFU) handleCandidate(client *Client, msg SignalMessage) {
	candidateData, _ := json.Marshal(msg.Candidate)
	var candidate webrtc.ICECandidate
	if err := json.Unmarshal(candidateData, &candidate); err != nil {
		log.Printf("SFU: Error parsing candidate: %v", err)
		return
	}

	// Check if remote description is set
	pc := client.Transport.GetPeerConnection()
	if pc == nil {
		log.Printf("SFU: Cannot handle candidate - peer connection is nil")
		return
	}

	client.mu.Lock()
	remoteDescSet := pc.RemoteDescription() != nil
	client.mu.Unlock()

	if !remoteDescSet {
		// Queue the candidate for later processing
		client.mu.Lock()
		client.pendingCandidates = append(client.pendingCandidates, &candidate)
		client.mu.Unlock()
		log.Printf("SFU: Queued ICE candidate for client %s (remote description not set yet)", client.ID)
		return
	}

	// Process candidate immediately
	signal := transport.SignalMessage{
		Type:      "candidate",
		Candidate: &candidate,
	}

	if err := client.Transport.Signal(signal); err != nil {
		log.Printf("SFU: Error handling candidate: %v", err)
	}
}

// processPendingCandidates processes any queued ICE candidates after remote description is set
func (s *SFU) processPendingCandidates(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.pendingCandidates) == 0 {
		return
	}

	log.Printf("SFU: Processing %d pending ICE candidates for client %s", len(client.pendingCandidates), client.ID)

	for _, candidate := range client.pendingCandidates {
		signal := transport.SignalMessage{
			Type:      "candidate",
			Candidate: candidate,
		}

		if err := client.Transport.Signal(signal); err != nil {
			log.Printf("SFU: Error processing pending candidate: %v", err)
		} else {
			log.Printf("SFU: Successfully processed pending candidate for client %s", client.ID)
		}
	}

	// Clear the queue
	client.pendingCandidates = client.pendingCandidates[:0]
}

// handleSubTrack handles track subscription (auto-subscription is now the default)
func (s *SFU) handleSubTrack(client *Client, msg SignalMessage) {
	log.Printf("SFU: Client %s subscribing to track %s from %s (auto-subscription active)", client.ID, msg.TrackID, msg.PubClientID)
	// Auto-subscription is handled in addTrackToClient - this handler is for manual subscription if needed
}

// handleUnsubTrack handles track unsubscription
func (s *SFU) handleUnsubTrack(client *Client, msg SignalMessage) {
	log.Printf("SFU: Client %s unsubscribing from track %s from %s", client.ID, msg.TrackID, msg.PubClientID)

	// Unsubscribe from the track
	params := SubParams{
		PubClientID: msg.PubClientID,
		RoomID:      client.RoomID,
		TrackID:     msg.TrackID,
		SubClientID: client.ID,
	}

	if err := s.tracksManager.Unsub(params); err != nil {
		log.Printf("SFU: Error unsubscribing from track: %v", err)
	}
}

func (s *SFU) handleQualityPreference(client *Client, msg SignalMessage) {
	quality := msg.Quality
	if quality == "" {
		quality = "auto"
	}

	client.mu.Lock()
	client.receiveQuality = quality

	pendingKeys := make([]string, 0, len(client.pendingSubs))
	for key := range client.pendingSubs {
		pendingKeys = append(pendingKeys, key)
	}

	activeSubs := make([]*activeVideoSub, 0, len(client.activeVideoSubs))
	for _, sub := range client.activeVideoSubs {
		activeSubs = append(activeSubs, sub)
	}
	client.mu.Unlock()

	for _, key := range pendingKeys {
		client.mu.Lock()
		pending, ok := client.pendingSubs[key]
		client.mu.Unlock()
		if !ok || pending.pubTrack.Kind != "video" {
			continue
		}

		selected, found := s.selectTrackForQuality(client.RoomID, quality, pending.pubTrack)
		if !found {
			continue
		}

		client.mu.Lock()
		if current, ok := client.pendingSubs[key]; ok {
			current.pubTrack = selected
		}
		client.mu.Unlock()
	}

	for _, sub := range activeSubs {
		selected, found := s.selectTrackForQuality(client.RoomID, quality, sub.pubTrack)
		if !found {
			continue
		}
		if selected.TrackID != sub.pubTrack.TrackID {
			s.switchActiveVideoLayer(client, sub, selected)
		}
	}

	log.Printf("SFU: Updated receive quality for client %s to %s", client.ID, quality)
}

// sendInitialOffer is no longer used - client sends the offer now
// This ensures proper transceiver alignment
func (s *SFU) sendInitialOffer(client *Client) {
	// DEPRECATED: Client now sends the offer first
	// This ensures the client's tracks are properly negotiated from the start
	log.Printf("SFU: sendInitialOffer called for client %s - DEPRECATED, client should send offer", client.ID)
}

// GetRoomStats returns statistics for monitoring
func (s *SFU) GetRoomStats(roomID string) (clientCount int, trackCount int) {
	return s.tracksManager.GetRoomStats(roomID)
}

// CleanupInactiveRooms removes rooms with no recent activity
func (s *SFU) CleanupInactiveRooms() {
	s.tracksManager.CleanupInactiveRooms()
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
