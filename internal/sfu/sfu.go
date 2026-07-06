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

// SFU represents the Selective Forwarding Unit server
type SFU struct {
	tracksManager *TracksManager
	upgrader      websocket.Upgrader
	mu            sync.RWMutex
	clients       map[string]*Client
}

// Client represents a connected SFU client
type Client struct {
	ID         string
	RoomID     string
	Conn       *websocket.Conn
	Transport  *transport.WebRTCTransport
	Negotiator *Negotiator
	mu         sync.Mutex // Protects renegotiation
	// Pending track subscriptions waiting for answer
	pendingSubs map[string]*pendingSub
	// Track events queue for initial connection
	initialTrackEvents []pubsub.PubTrackEvent
	// Flag to indicate if initial connection is established
	initialConnected bool
	// Pending ICE candidates waiting for remote description
	pendingCandidates []*webrtc.ICECandidate
	// Channel to signal when local description is set (for ICE candidates)
	descriptionSent     chan struct{}
	descriptionSentOnce sync.Once
<<<<<<< HEAD
	// Phase 10: ICE restart state
	iceRestartPending bool
	iceRestartCount   int
	// Phase 9: Connection monitoring
	lastActivity    time.Time
	connectionState webrtc.PeerConnectionState
	// Write channel for serializing WebSocket writes
	writeCh chan interface{}
=======
>>>>>>> main
}

// pendingSub represents a pending track subscription
type pendingSub struct {
	pubTrack   pubsub.PubTrack
	trackLocal *trackLocalImpl
	rtcpReader *rtcpReaderImpl
	rtpSender  *webrtc.RTPSender
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

<<<<<<< HEAD
	// NOTE: We no longer create upfront transceivers.
	// The client will send an offer with its tracks, and we'll create matching transceivers.
	// This ensures proper transceiver alignment between client and SFU.
	pc := webrtcTransport.GetPeerConnection()
	if pc == nil {
		log.Printf("SFU: ERROR - PeerConnection is nil for client %s", clientID)
=======
	// Add sendrecv transceivers to receive and send audio and video
	// Using sendrecv because the SFU needs to:
	// 1. Receive tracks from this client to forward to others
	// 2. Send tracks from other clients to this client
	pc := webrtcTransport.GetPeerConnection()
	if pc != nil {
		// Add audio transceiver (sendrecv - we receive audio from client and send audio from other clients)
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		}); err != nil {
			log.Printf("SFU: Failed to add audio transceiver for client %s: %v", clientID, err)
		} else {
			log.Printf("SFU: Added sendrecv audio transceiver for client %s", clientID)
		}

		// Add video transceiver (sendrecv - we receive video from client and send video from other clients)
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendrecv,
		}); err != nil {
			log.Printf("SFU: Failed to add video transceiver for client %s: %v", clientID, err)
		} else {
			log.Printf("SFU: Added sendrecv video transceiver for client %s", clientID)
		}
>>>>>>> main
	}

	client := &Client{

		ID:                 clientID,
		RoomID:             roomID,
		Conn:               conn,
		Transport:          webrtcTransport,
		pendingSubs:        make(map[string]*pendingSub),
		initialTrackEvents: make([]pubsub.PubTrackEvent, 0),
		initialConnected:   false,
		pendingCandidates:  make([]*webrtc.ICECandidate, 0),
		descriptionSent:    make(chan struct{}),
<<<<<<< HEAD
		lastActivity:       time.Now(),
		connectionState:    webrtc.PeerConnectionStateNew,
		writeCh:            make(chan interface{}, 256),
	}

	// Start dedicated write goroutine to serialize WebSocket writes
	go s.writePump(client)
=======
	}

	// Create negotiator following peer-calls pattern
	// The SFU is always the initiator (server), clients are non-initiators
	client.Negotiator = NewNegotiator(
		true, // SFU is initiator
		webrtcTransport.GetPeerConnection(),
		func(offer webrtc.SessionDescription, err error) {
			s.handleLocalOffer(client, offer, err)
		},
		func() {
			// Non-initiators request negotiation - not used for SFU
			log.Printf("SFU: Received negotiation request from client %s", clientID)
		},
	)
>>>>>>> main

	s.mu.Lock()
	s.clients[clientID] = client
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

	// Phase 9: Start connection monitoring
	go s.monitorConnection(client)

	// Handle client messages (this will wait for ready signal before sending offer)
	s.handleClientMessages(client)
}

<<<<<<< HEAD
// writePump serializes WebSocket writes through a channel
func (s *SFU) writePump(client *Client) {
	for msg := range client.writeCh {
		switch m := msg.(type) {
		case string:
			// Ping message
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("SFU: Error sending ping to client %s: %v", client.ID, err)
				return
			}
		default:
			// JSON message
			if err := client.Conn.WriteJSON(m); err != nil {
				log.Printf("SFU: Error writing to client %s: %v", client.ID, err)
				return
			}
		}
	}
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
				select {
				case client.writeCh <- "ping":
				default:
					log.Printf("SFU: Failed to queue ping for client %s", client.ID)
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
	s.mu.Lock()
	delete(s.clients, client.ID)
	s.mu.Unlock()

	client.Conn.Close()
	client.Transport.Close()
	log.Printf("SFU: Closed client %s connection", client.ID)
=======
// handleLocalOffer handles the local offer created by the negotiator
func (s *SFU) handleLocalOffer(client *Client, offer webrtc.SessionDescription, err error) {
	if err != nil {
		log.Printf("SFU: Error creating offer for client %s: %v", client.ID, err)
		return
	}

	log.Printf("SFU: Local offer created for client %s (SDP length=%d)", client.ID, len(offer.SDP))

	// Debug: log current transceiver state on the peer connection
	pc := client.Transport.GetPeerConnection()
	if pc != nil {
		trs := pc.GetTransceivers()
		log.Printf("SFU: PeerConnection has %d transceivers", len(trs))
		for i, tr := range trs {
			senderExists := false
			if tr.Sender() != nil && tr.Sender().Track() != nil {
				senderExists = true
			}
			log.Printf("SFU: Transceiver[%d] kind=%v direction=%v senderTrackPresent=%v", i, tr.Kind(), tr.Direction(), senderExists)
		}
	}

	// Set local description
	pc = client.Transport.GetPeerConnection()
	if err := pc.SetLocalDescription(offer); err != nil {
		log.Printf("SFU: Error setting local description for client %s: %v", client.ID, err)
		return
	}

	// Signal that local description is set (allows ICE candidates to be sent)
	client.descriptionSentOnce.Do(func() {
		close(client.descriptionSent)
	})

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(pc)

	select {
	case <-gatherComplete:
		log.Printf("SFU: ICE gathering completed for client %s", client.ID)
	case <-time.After(10000 * time.Second):
		log.Printf("SFU: ICE gathering timeout for client %s, proceeding", client.ID)
	}

	// Get the final local description with ICE candidates
	localDesc := pc.LocalDescription()
	if localDesc == nil {
		log.Printf("SFU: Local description is nil after offer creation for client %s", client.ID)
		return
	}

	// Debug: print a truncated view of the offer SDP to inspect m-lines / ICE attributes
	if len(localDesc.SDP) > 0 {
		if len(localDesc.SDP) > 1000 {
			log.Printf("SFU: Offer SDP (truncated 1000 chars):\n%s", localDesc.SDP[:1000])
		} else {
			log.Printf("SFU: Offer SDP:\n%s", localDesc.SDP)
		}
	}

	// Send offer to client
	msg := SignalMessage{
		Type:  "offer",
		Offer: localDesc.SDP,
	}

	client.mu.Lock()
	if err := client.Conn.WriteJSON(msg); err != nil {
		log.Printf("SFU: Error sending offer to client %s: %v", client.ID, err)
		client.mu.Unlock()
		return
	}
	client.mu.Unlock()

	log.Printf("SFU: Sent offer to client %s (SDP length=%d)", client.ID, len(localDesc.SDP))
>>>>>>> main
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
		case client.writeCh <- msg:
		default:
			log.Printf("SFU: WARNING - dropping pub track event for client %s: write channel full", client.ID)
		}

		// If this is a new track from another peer, add it to this client's peer connection
		if event.Type == pubsub.TrackEventTypeAdd && event.PubTrack.ClientID != client.ID {
			log.Printf("SFU: Track ADD event - track %s (kind=%s) from %s for client %s, initialConnected=%v",
				event.PubTrack.TrackID, event.PubTrack.Kind, event.PubTrack.ClientID, client.ID, client.initialConnected)

			// If initial connection not yet established, queue the track event
			client.mu.Lock()
			if !client.initialConnected {
				log.Printf("SFU: QUEUING track %s from %s for client %s (initial connection pending)",
					event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID)
				client.initialTrackEvents = append(client.initialTrackEvents, event)
				client.mu.Unlock()
				continue
			}
			client.mu.Unlock()

			// Get the track from the publisher and add it to this client
			log.Printf("SFU: IMMEDIATELY adding track %s from %s to client %s",
				event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID)
			go s.addTrackToClient(client, event.PubTrack)
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

	var addedAny bool
	for _, event := range client.initialTrackEvents {
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
		case client.writeCh <- msg:
			log.Printf("SFU: Sent batch renegotiation offer to client %s", client.ID)
		default:
			log.Printf("SFU: [BATCH] Error sending offer: write channel full")
		}
	}
}

<<<<<<< HEAD
// addTrackInternal performs the track setup without triggering a renegotiation offer.
// Must be called with client.mu locked.
func (s *SFU) addTrackInternal(client *Client, pubTrack pubsub.PubTrack) (*webrtc.RTPSender, error) {
	log.Printf("SFU: [ADD TRACK INTERNAL] track %s (kind=%s) from %s to client %s", pubTrack.TrackID, pubTrack.Kind, pubTrack.ClientID, client.ID)
=======
// addTrackToClient adds a published track to a client's peer connection
// Following peer-calls pattern: use negotiator to queue transceiver and trigger renegotiation
func (s *SFU) addTrackToClient(client *Client, pubTrack pubsub.PubTrack) {
	log.Printf("SFU: Adding track %s from %s to client %s", pubTrack.TrackID, pubTrack.ClientID, client.ID)
>>>>>>> main

	// Check if already subscribed to this track
	client.mu.Lock()
	if _, exists := client.pendingSubs[pubTrack.TrackID]; exists {
		log.Printf("SFU: Track %s already pending for client %s, skipping", pubTrack.TrackID, client.ID)
		client.mu.Unlock()
		return
	}
	client.mu.Unlock()

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

	streamID := fmt.Sprintf("pub-%s-%s", pubTrack.ClientID, pubTrack.TrackID)
	trackToForward, err := webrtc.NewTrackLocalStaticRTP(
		codecCapability,
		pubTrack.TrackID,
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

<<<<<<< HEAD
	// Store as pending subscription
=======
	// Create track local wrapper for subscription
	trackLocal := &trackLocalImpl{
		track: trackToForward,
	}

	// Store as pending subscription
	client.mu.Lock()
>>>>>>> main
	client.pendingSubs[pubTrack.TrackID] = &pendingSub{
		pubTrack:   pubTrack,
		trackLocal: trackLocal,
		rtcpReader: rtcpReader,
		rtpSender:  rtpSender,
	}
	client.mu.Unlock()

	// Start RTCP processing
	go s.processRTCP(rtpSender)

<<<<<<< HEAD
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
=======
	// IMPORTANT: Subscribe immediately so packets start flowing
	// The subscription must happen BEFORE renegotiation completes
	subParams := SubParams{
		PubClientID: pubTrack.ClientID,
		RoomID:      client.RoomID,
		TrackID:     pubTrack.TrackID,
		SubClientID: client.ID,
	}

	if err := s.tracksManager.Sub(subParams, trackLocal, rtcpReader); err != nil {
		log.Printf("SFU: Error subscribing to track %s: %v", pubTrack.TrackID, err)
		// Clean up
		client.mu.Lock()
>>>>>>> main
		delete(client.pendingSubs, pubTrack.TrackID)
		client.mu.Unlock()
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	}

	log.Printf("SFU: Successfully subscribed client %s to track %s (pre-negotiation)", client.ID, pubTrack.TrackID)

<<<<<<< HEAD
	select {
	case client.writeCh <- msg:
		log.Printf("SFU: Sent renegotiation offer to client %s for track %s", client.ID, pubTrack.TrackID)
	default:
		log.Printf("SFU: [ADD TRACK] Error sending offer: write channel full")
		// Clean up pending sub
		delete(client.pendingSubs, pubTrack.TrackID)
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
	}
=======
	// Use the negotiator to trigger renegotiation
	// This follows the peer-calls pattern for proper offer/answer sequencing
	client.Negotiator.Negotiate()
>>>>>>> main
}

// completePendingSubscriptions handles any cleanup after answer is received
// Note: Subscriptions now happen immediately in addTrackToClient
func (s *SFU) completePendingSubscriptions(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

<<<<<<< HEAD
	log.Printf("SFU: [COMPLETE PENDING] START for client %s, pending count=%d", client.ID, len(client.pendingSubs))

	// Log all pending track IDs
	for trackID, pending := range client.pendingSubs {
		log.Printf("SFU: [COMPLETE PENDING] Pending track: %s (kind=%s) from %s",
			trackID, pending.pubTrack.Kind, pending.pubTrack.ClientID)
	}

	for trackID, pending := range client.pendingSubs {
		log.Printf("SFU: [COMPLETE PENDING] Processing track %s (kind=%s) from %s for client %s",
			trackID, pending.pubTrack.Kind, pending.pubTrack.ClientID, client.ID)

		// Subscribe to the track through the tracks manager
		// This connects the PubSub system to forward RTP packets to this writer
		subParams := SubParams{
			PubClientID: pending.pubTrack.ClientID,
			RoomID:      client.RoomID,
			TrackID:     trackID,
			SubClientID: client.ID,
		}

		if err := s.tracksManager.Sub(subParams, pending.trackLocal, pending.rtcpReader); err != nil {
			log.Printf("SFU: [COMPLETE PENDING] ERROR subscribing to track %s: %v", trackID, err)
			// Remove the track we added
			client.Transport.GetPeerConnection().RemoveTrack(pending.rtpSender)
		} else {
			log.Printf("SFU: [COMPLETE PENDING] SUCCESS - client %s subscribed to track %s (kind=%s) from %s",
				client.ID, trackID, pending.pubTrack.Kind, pending.pubTrack.ClientID)

			// CRITICAL: Request a keyframe from the publisher to ensure the subscriber
			// receives a valid video stream immediately
			if pending.pubTrack.Kind == "video" {
				log.Printf("SFU: [COMPLETE PENDING] Requesting keyframe for video track %s from %s", trackID, pending.pubTrack.ClientID)
				s.requestKeyframe(pending.pubTrack.ClientID, trackID)
			}
		}

		// Remove from pending
=======
	// Just clean up the pending map - subscriptions are already active
	for trackID := range client.pendingSubs {
		log.Printf("SFU: Completing subscription for track %s (already active)", trackID)
>>>>>>> main
		delete(client.pendingSubs, trackID)
	}

	log.Printf("SFU: [COMPLETE PENDING] END for client %s", client.ID)
}

// requestKeyframe sends a PLI to the publisher to request a keyframe
func (s *SFU) requestKeyframe(pubClientID, trackID string) {
	s.mu.RLock()
	pubClient, ok := s.clients[pubClientID]
	s.mu.RUnlock()

	if !ok {
		log.Printf("SFU: Cannot request keyframe - publisher %s not found", pubClientID)
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
		log.Printf("SFU: Successfully requested keyframe from %s for track %s", pubClientID, trackID)
	}
}

// processRTCP processes RTCP packets for a sender and forwards PLI to publisher

func (s *SFU) processRTCP(rtpSender *webrtc.RTPSender) {
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
				// CRITICAL: PLI received from subscriber - forward to publisher
				log.Printf("SFU: Received PLI from subscriber for SSRC %d - forwarding to publisher", p.MediaSSRC)
				// Forward PLI to the publisher to request a keyframe
				s.forwardPLIToPublisher(p)
			case *rtcp.ReceiverEstimatedMaximumBitrate:
<<<<<<< HEAD
				log.Printf("SFU: Received REMB from subscriber: %f bps", p.Bitrate)
			case *rtcp.TransportLayerNack:
				// NACK packets indicate packet loss - normal for video
				log.Printf("SFU: Received NACK from subscriber for SSRC %d", p.MediaSSRC)
=======
>>>>>>> main
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
		case client.writeCh <- msg:
		default:
			log.Printf("SFU: Error sending signal: write channel full")
			return
		}
	}
}

// handleClientMessages handles incoming WebSocket messages
func (s *SFU) handleClientMessages(client *Client) {
<<<<<<< HEAD
	// Channel to signal when to stop
	done := make(chan struct{})

	defer func() {
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()

		// Close signaling channel and connection
		close(done)
		close(client.writeCh)
		client.Conn.Close()

		client.Transport.Close()
		log.Printf("SFU: Client %s disconnected and cleanup completed", client.ID)
	}()

=======
>>>>>>> main
	// Set up ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

<<<<<<< HEAD
	// Reconnection tracking
	reconnectAttempts := 0
	maxReconnectAttempts := 3
=======
	// Channel to signal when to stop - use a sync.Once to prevent double close
	var stopOnce sync.Once
	stopCh := make(chan struct{})
	stop := func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
	defer stop()
>>>>>>> main

	// Start ping goroutine - uses writeCh for serialization
	go func() {
		for {
			select {
			case <-pingTicker.C:
<<<<<<< HEAD
				select {
				case client.writeCh <- "ping":
				default:
					log.Printf("SFU: Error sending ping to client %s: write channel full", client.ID)
=======
				client.mu.Lock()
				conn := client.Conn
				client.mu.Unlock()
				if conn == nil {
					return
				}
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("SFU: Error sending ping to client %s: %v", client.ID, err)
>>>>>>> main
					return
				}
			case <-stopCh:
				return
			}
		}
	}()

	// Ensure cleanup happens properly
	defer func() {
		log.Printf("SFU: Cleaning up client %s", client.ID)

		// Stop the ping goroutine (safe to call multiple times)
		stop()

		// Remove from clients map
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()

		// Close WebSocket
		client.mu.Lock()
		if client.Conn != nil {
			client.Conn.Close()
			client.Conn = nil
		}
		client.mu.Unlock()

		// Close transport
		if client.Transport != nil {
			client.Transport.Close()
		}

		log.Printf("SFU: Client %s fully disconnected", client.ID)
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
				select {
				case client.writeCh <- SignalMessage{Type: "reconnect", ClientID: client.ID}:
					log.Printf("SFU: Sent reconnect signal to client %s", client.ID)
				default:
					// Write channel full, just break
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
<<<<<<< HEAD
				log.Printf("SFU: Client %s is ready, waiting for client offer", client.ID)
				// Client will send offer - we don't send initial offer anymore
				// This ensures the client's tracks are properly negotiated
=======
				log.Printf("SFU: Client %s is ready, starting negotiation", client.ID)
				// Use negotiator to start initial negotiation
				client.Negotiator.Negotiate()
>>>>>>> main
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
		case "leave":
			return
		case "error":
			// Handle error messages from client gracefully
			log.Printf("SFU: Received error from client %s: %v", client.ID, msg)
			// Don't disconnect, just log the error
		default:
			log.Printf("SFU: Unknown message type: %s from client %s", msg.Type, client.ID)
		}

	}
}

// handleOffer handles an offer message (from non-initiator client)
func (s *SFU) handleOffer(client *Client, msg SignalMessage) {
	log.Printf("SFU: Received offer from client %s (len=%d)", client.ID, len(msg.Offer))

	signal := transport.SignalMessage{
		Type: "offer",
		SDP:  msg.Offer,
	}

	if err := client.Transport.Signal(signal); err != nil {
<<<<<<< HEAD
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
=======
		log.Printf("SFU: Error handling offer from client %s: %v", client.ID, err)
>>>>>>> main
	}
}

// handleAnswer handles an answer message
func (s *SFU) handleAnswer(client *Client, msg SignalMessage) {
	log.Printf("SFU: Received answer from client %s (len=%d)", client.ID, len(msg.Answer))

<<<<<<< HEAD
	// Phase 10: Check if this is an ICE restart answer
	if msg.ICERestart {
		log.Printf("SFU: Received ICE restart answer from client %s", client.ID)
		client.mu.Lock()
		client.iceRestartPending = false
		client.mu.Unlock()
	}

=======
>>>>>>> main
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

<<<<<<< HEAD
// sendInitialOffer is no longer used - client sends the offer now
// This ensures proper transceiver alignment
func (s *SFU) sendInitialOffer(client *Client) {
	// DEPRECATED: Client now sends the offer first
	// This ensures the client's tracks are properly negotiated from the start
	log.Printf("SFU: sendInitialOffer called for client %s - DEPRECATED, client should send offer", client.ID)
}

=======
>>>>>>> main
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
