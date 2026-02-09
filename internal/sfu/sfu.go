package sfu

import (
	"context"
	"encoding/json"
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
	ID        string
	RoomID    string
	Conn      *websocket.Conn
	Transport *transport.WebRTCTransport
	mu        sync.Mutex // Protects renegotiation
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
	// Phase 10: ICE restart state
	iceRestartPending bool
	iceRestartCount   int
	// Phase 9: Connection monitoring
	lastActivity    time.Time
	connectionState webrtc.PeerConnectionState
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

	// Add transceivers for audio and video to the peer connection
	// CRITICAL: Use recvonly direction so the client sends media to the SFU
	// The client will add its tracks and send them to these recvonly transceivers
	pc := webrtcTransport.GetPeerConnection()
	if pc != nil {
		// Add recvonly transceivers - SFU receives from client, client sends to SFU
		_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		})
		if err != nil {
			log.Printf("SFU: Failed to add audio transceiver: %v", err)
		}

		_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		})
		if err != nil {
			log.Printf("SFU: Failed to add video transceiver: %v", err)
		}

		log.Printf("SFU: Added recvonly transceivers for audio and video to client %s (SFU will receive from client)", clientID)
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
		lastActivity:       time.Now(),
		connectionState:    webrtc.PeerConnectionStateNew,
	}

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
			client.mu.Unlock()

			if inactive && state != webrtc.PeerConnectionStateClosed {
				log.Printf("SFU: Client %s inactive for 60 seconds, checking health", client.ID)
				// Send a keepalive ping via WebSocket
				if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("SFU: Failed to ping client %s: %v", client.ID, err)
				}
			}
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

	client.mu.Lock()
	if err := client.Conn.WriteJSON(msg); err != nil {
		log.Printf("SFU: Failed to send ICE restart offer to client %s: %v", client.ID, err)
		client.mu.Unlock()
		client.mu.Lock()
		client.iceRestartPending = false
		client.mu.Unlock()
		return
	}
	client.mu.Unlock()

	log.Printf("SFU: Sent ICE restart offer to client %s", client.ID)
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
}

// handlePubTrackEvents handles track publication events
func (s *SFU) handlePubTrackEvents(client *Client, eventsCh <-chan pubsub.PubTrackEvent) {
	for event := range eventsCh {
		// Send event to client via WebSocket
		msg := PubTrackMessage{
			Type:        string(event.Type),
			PubClientID: event.PubTrack.ClientID,
			TrackID:     event.PubTrack.TrackID,
			Kind:        event.PubTrack.Kind,
		}

		if err := client.Conn.WriteJSON(msg); err != nil {
			log.Printf("SFU: Error sending pub track event: %v", err)
			return
		}

		// If this is a new track from another peer, add it to this client's peer connection

		if event.Type == pubsub.TrackEventTypeAdd && event.PubTrack.ClientID != client.ID {
			log.Printf("SFU: Track event received - track %s from %s for client %s, initialConnected=%v",
				event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID, client.initialConnected)

			// If initial connection not yet established, queue the track event
			client.mu.Lock()
			if !client.initialConnected {
				log.Printf("SFU: Queuing track %s from %s for later (initial connection pending)",
					event.PubTrack.TrackID, event.PubTrack.ClientID)
				client.initialTrackEvents = append(client.initialTrackEvents, event)
				client.mu.Unlock()
				continue
			}
			client.mu.Unlock()

			// Get the track from the publisher and add it to this client
			go s.addTrackToClient(client, event.PubTrack)
		}
	}
}

// processQueuedTrackEvents processes any track events that were queued during initial connection
func (s *SFU) processQueuedTrackEvents(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	log.Printf("SFU: Processing %d queued track events for client %s", len(client.initialTrackEvents), client.ID)

	for _, event := range client.initialTrackEvents {
		log.Printf("SFU: Processing queued track %s from %s for client %s",
			event.PubTrack.TrackID, event.PubTrack.ClientID, client.ID)
		go s.addTrackToClient(client, event.PubTrack)
	}

	// Clear the queue
	client.initialTrackEvents = client.initialTrackEvents[:0]
}

// addTrackToClient adds a published track to a client's peer connection
func (s *SFU) addTrackToClient(client *Client, pubTrack pubsub.PubTrack) {
	// Lock to prevent concurrent renegotiations for the same client
	client.mu.Lock()
	defer client.mu.Unlock()

	log.Printf("SFU: Adding track %s from %s to client %s", pubTrack.TrackID, pubTrack.ClientID, client.ID)

	// Get codec capability based on track kind
	var codecCapability webrtc.RTPCodecCapability
	switch pubTrack.Kind {
	case "video":
		codecCapability = webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8,
		}
	case "audio":
		codecCapability = webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus,
		}
	default:
		log.Printf("SFU: Unknown track kind: %v", pubTrack.Kind)
		return
	}

	// Create a local track to forward the published track
	trackToForward, err := webrtc.NewTrackLocalStaticRTP(
		codecCapability,
		pubTrack.TrackID,
		pubTrack.ClientID, // Use publisher's client ID as stream ID for identification
	)
	if err != nil {
		log.Printf("SFU: Error creating forward track: %v", err)
		return
	}

	// Add the track to the subscriber's peer connection
	rtpSender, err := client.Transport.GetPeerConnection().AddTrack(trackToForward)
	if err != nil {
		log.Printf("SFU: Error adding track to subscriber: %v", err)
		return
	}

	// Create RTCP reader for this sender
	rtcpReader := &rtcpReaderImpl{sender: rtpSender}

	// Create track local wrapper for subscription
	trackLocal := &trackLocalImpl{
		track: trackToForward,
	}

	// Store as pending subscription - will be activated after answer is received
	client.pendingSubs[pubTrack.TrackID] = &pendingSub{
		pubTrack:   pubTrack,
		trackLocal: trackLocal,
		rtcpReader: rtcpReader,
		rtpSender:  rtpSender,
	}

	// Start RTCP processing for this sender
	go s.processRTCP(rtpSender)

	// Renegotiate - create and send offer
	offer, err := client.Transport.CreateOffer()
	if err != nil {
		log.Printf("SFU: Error creating offer for track addition: %v", err)
		// Clean up pending sub
		delete(client.pendingSubs, pubTrack.TrackID)
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	}

	// Send offer to client
	msg := SignalMessage{
		Type:  "offer",
		Offer: offer.SDP,
	}

	if err := client.Conn.WriteJSON(msg); err != nil {
		log.Printf("SFU: Error sending offer for track addition: %v", err)
		// Clean up pending sub
		delete(client.pendingSubs, pubTrack.TrackID)
		client.Transport.GetPeerConnection().RemoveTrack(rtpSender)
		return
	}

	log.Printf("SFU: Sent renegotiation offer to client %s for track %s", client.ID, pubTrack.TrackID)
}

// completePendingSubscriptions completes pending track subscriptions after answer is received
func (s *SFU) completePendingSubscriptions(client *Client) {
	client.mu.Lock()
	defer client.mu.Unlock()

	for trackID, pending := range client.pendingSubs {
		// Subscribe to the track through the tracks manager
		// This connects the PubSub system to forward RTP packets to this writer
		subParams := SubParams{
			PubClientID: pending.pubTrack.ClientID,
			RoomID:      client.RoomID,
			TrackID:     trackID,
			SubClientID: client.ID,
		}

		if err := s.tracksManager.Sub(subParams, pending.trackLocal, pending.rtcpReader); err != nil {
			log.Printf("SFU: Error subscribing to track %s: %v", trackID, err)
			// Remove the track we added
			client.Transport.GetPeerConnection().RemoveTrack(pending.rtpSender)
		} else {
			log.Printf("SFU: Successfully subscribed client %s to track %s from %s", client.ID, trackID, pending.pubTrack.ClientID)
		}

		// Remove from pending
		delete(client.pendingSubs, trackID)
	}
}

// processRTCP processes RTCP packets for a sender
func (s *SFU) processRTCP(rtpSender *webrtc.RTPSender) {
	rtcpBuf := make([]byte, 1500)
	for {
		n, _, err := rtpSender.Read(rtcpBuf)
		if err != nil {
			return
		}

		// Parse RTCP packets for debugging
		packets, err := rtcp.Unmarshal(rtcpBuf[:n])
		if err != nil {
			continue
		}

		for _, packet := range packets {
			switch p := packet.(type) {
			case *rtcp.PictureLossIndication:
				log.Printf("SFU: Received PLI from subscriber for SSRC %d", p.MediaSSRC)
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				log.Printf("SFU: Received REMB from subscriber: %f", p.Bitrate)
			}
		}
	}
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

		if err := client.Conn.WriteJSON(msg); err != nil {
			log.Printf("SFU: Error sending signal: %v", err)
			return
		}
	}
}

// handleClientMessages handles incoming WebSocket messages
func (s *SFU) handleClientMessages(client *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()

		client.Conn.Close()

		client.Transport.Close()
		log.Printf("SFU: Client %s disconnected", client.ID)
	}()

	// Set up ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Channel to signal when to stop
	done := make(chan struct{})
	defer close(done)

	// Start ping goroutine
	go func() {
		for {
			select {
			case <-pingTicker.C:
				if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("SFU: Error sending ping to client %s: %v", client.ID, err)
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
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
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
				log.Printf("SFU: Client %s is ready, sending initial offer", client.ID)
				go s.sendInitialOffer(client)
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
	client.mu.Unlock()

	// Complete any pending track subscriptions now that renegotiation is done
	s.completePendingSubscriptions(client)

	// If this was the initial connection, process any queued track events
	if wasInitial {
		s.processQueuedTrackEvents(client)
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

// sendInitialOffer sends the initial offer to a newly connected client
func (s *SFU) sendInitialOffer(client *Client) {
	// Small delay to ensure everything is ready
	time.Sleep(100 * time.Millisecond)

	// Create initial offer
	offer, err := client.Transport.CreateOffer()
	if err != nil {
		log.Printf("SFU: Error creating initial offer for client %s: %v", client.ID, err)
		return
	}

	log.Printf("SFU: Created initial offer for client %s (len=%d)", client.ID, len(offer.SDP))

	// Send offer to client
	msg := SignalMessage{
		Type:  "offer",
		Offer: offer.SDP,
	}

	// Use write lock to prevent concurrent writes
	client.mu.Lock()
	if err := client.Conn.WriteJSON(msg); err != nil {
		log.Printf("SFU: Error sending initial offer to client %s: %v", client.ID, err)
		client.mu.Unlock()
		return
	}
	client.mu.Unlock()

	log.Printf("SFU: Sent initial offer to client %s, waiting for answer", client.ID)
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
