package sfu

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type SFU struct {
	rooms    map[string]*Room
	clients  map[string]*Client
	mu       sync.RWMutex
	upgrader websocket.Upgrader
}

type Room struct {
	ID           string
	Clients      map[string]*Client
	Tracks       map[string]*webrtc.TrackLocalStaticRTP
	mu           sync.RWMutex
	CreatedAt    time.Time
	LastActivity time.Time
}

type Client struct {
	ID       string
	RoomID   string
	Conn     *websocket.Conn
	PeerConn *webrtc.PeerConnection
	Tracks   map[string]*webrtc.TrackRemote
	mu       sync.RWMutex
}

type SignalMessage struct {
	Type      string                 `json:"type"`
	ClientID  string                 `json:"client_id,omitempty"`
	RoomID    string                 `json:"room_id,omitempty"`
	Data      interface{}            `json:"data,omitempty"`
	Offer     string                 `json:"offer,omitempty"`
	Answer    string                 `json:"answer,omitempty"`
	Candidate map[string]interface{} `json:"candidate,omitempty"`
}

func NewSFU() *SFU {
	return &SFU{
		rooms:   make(map[string]*Room),
		clients: make(map[string]*Client),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

func New(ctx context.Context, opts interface{}) *SFU {
	return NewSFU()
}

func DefaultOptions() interface{} {
	return nil
}

func (s *SFU) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	roomID := r.URL.Query().Get("room_id")

	if clientID == "" || roomID == "" {
		http.Error(w, "client_id and room_id required", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	client := &Client{
		ID:     clientID,
		RoomID: roomID,
		Conn:   conn,
		Tracks: make(map[string]*webrtc.TrackRemote),
	}

	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	// Ensure room exists
	s.mu.Lock()
	if s.rooms[roomID] == nil {
		s.rooms[roomID] = &Room{
			ID:           roomID,
			Clients:      make(map[string]*Client),
			Tracks:       make(map[string]*webrtc.TrackLocalStaticRTP),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
	}
	room := s.rooms[roomID]
	room.Clients[clientID] = client
	s.mu.Unlock()

	log.Printf("Client %s joined room %s", clientID, roomID)

	// Initialize peer connection for client
	s.handleJoin(client)

	// Handle client messages
	go s.handleClient(client)
}

func (s *SFU) handleClient(client *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client.ID)
		if room, exists := s.rooms[client.RoomID]; exists {
			delete(room.Clients, client.ID)
			// Clean up empty rooms after some time
			if len(room.Clients) == 0 {
				go s.cleanupRoom(client.RoomID)
			}
		}
		s.mu.Unlock()
		client.Conn.Close()
	}()

	for {
		var msg SignalMessage
		err := client.Conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("Error reading message from client %s: %v", client.ID, err)
			break
		}

		// Update room activity
		s.mu.RLock()
		if room, exists := s.rooms[client.RoomID]; exists {
			room.mu.Lock()
			room.LastActivity = time.Now()
			room.mu.Unlock()
		}
		s.mu.RUnlock()

		switch msg.Type {
		case "offer":
			s.handleOffer(client, msg)
		case "answer":
			s.handleAnswer(client, msg)
		case "candidate":
			s.handleCandidate(client, msg)
		case "leave":
			return
		default:
			// Ignore unknown message types (e.g., room management messages)
			log.Printf("SFU ignoring unknown message type: %s from client %s", msg.Type, client.ID)
		}
	}
}

func (s *SFU) handleJoin(client *Client) {
	// Create peer connection for client
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("Failed to create peer connection for client %s: %v", client.ID, err)
		return
	}

	client.PeerConn = peerConn

	// Handle ICE candidates
	peerConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		client.Conn.WriteJSON(SignalMessage{
			Type:     "candidate",
			ClientID: client.ID,
			Candidate: map[string]interface{}{
				"candidate":     candidate.ToJSON().Candidate,
				"sdpMid":        candidate.ToJSON().SDPMid,
				"sdpMLineIndex": candidate.ToJSON().SDPMLineIndex,
			},
		})
	})

	// Handle incoming tracks
	peerConn.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Received track %s from client %s", track.ID(), client.ID)

		client.mu.Lock()
		client.Tracks[track.ID()] = track
		client.mu.Unlock()

		// Forward track to other clients in room
		s.forwardTrack(client.RoomID, client.ID, track)
	})

	// Add existing tracks to new client
	s.mu.RLock()
	room := s.rooms[client.RoomID]
	s.mu.RUnlock()

	room.mu.RLock()
	for _, existingTrack := range room.Tracks {
		rtpSender, err := peerConn.AddTrack(existingTrack)
		if err != nil {
			log.Printf("Failed to add existing track: %v", err)
			continue
		}
		go s.processRTCP(rtpSender)
	}
	room.mu.RUnlock()
}

func (s *SFU) handleOffer(client *Client, msg SignalMessage) {
	if client.PeerConn == nil {
		log.Printf("Peer connection not initialized for client %s", client.ID)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.Offer,
	}

	err := client.PeerConn.SetRemoteDescription(offer)
	if err != nil {
		log.Printf("Failed to set remote description: %v", err)
		return
	}

	answer, err := client.PeerConn.CreateAnswer(nil)
	if err != nil {
		log.Printf("Failed to create answer: %v", err)
		return
	}

	err = client.PeerConn.SetLocalDescription(answer)
	if err != nil {
		log.Printf("Failed to set local description: %v", err)
		return
	}

	client.Conn.WriteJSON(SignalMessage{
		Type:     "answer",
		ClientID: client.ID,
		Answer:   answer.SDP,
	})
}

func (s *SFU) handleAnswer(client *Client, msg SignalMessage) {
	if client.PeerConn == nil {
		return
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.Answer,
	}

	err := client.PeerConn.SetRemoteDescription(answer)
	if err != nil {
		log.Printf("Failed to set remote description: %v", err)
	}
}

func (s *SFU) handleCandidate(client *Client, msg SignalMessage) {
	if client.PeerConn == nil {
		return
	}

	sdpMid := msg.Candidate["sdpMid"].(string)
	sdpMLineIndex := uint16(msg.Candidate["sdpMLineIndex"].(float64))

	candidate := webrtc.ICECandidateInit{
		Candidate:     msg.Candidate["candidate"].(string),
		SDPMid:        &sdpMid,
		SDPMLineIndex: &sdpMLineIndex,
	}

	err := client.PeerConn.AddICECandidate(candidate)
	if err != nil {
		log.Printf("Failed to add ICE candidate: %v", err)
	}
}

func (s *SFU) forwardTrack(roomID, senderID string, track *webrtc.TrackRemote) {
	s.mu.RLock()
	room, exists := s.rooms[roomID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	// Create local track for forwarding
	localTrack, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID(), track.StreamID())
	if err != nil {
		log.Printf("Failed to create local track: %v", err)
		return
	}

	room.mu.Lock()
	room.Tracks[track.ID()] = localTrack
	room.mu.Unlock()

	// Add track to all other clients in room
	room.mu.RLock()
	for clientID, client := range room.Clients {
		if clientID == senderID || client.PeerConn == nil {
			continue
		}

		rtpSender, err := client.PeerConn.AddTrack(localTrack)
		if err != nil {
			log.Printf("Failed to add track to client %s: %v", clientID, err)
			continue
		}
		go s.processRTCP(rtpSender)
	}
	room.mu.RUnlock()

	// Forward RTP packets
	go func() {
		rtpBuf := make([]byte, 1600)
		for {
			i, _, err := track.Read(rtpBuf)
			if err != nil {
				log.Printf("Track read error: %v", err)
				return
			}

			if _, err := localTrack.Write(rtpBuf[:i]); err != nil {
				log.Printf("Track write error: %v", err)
				return
			}
		}
	}()
}

func (s *SFU) processRTCP(rtpSender *webrtc.RTPSender) {
	rtcpBuf := make([]byte, 1500)
	for {
		if _, _, err := rtpSender.Read(rtcpBuf); err != nil {
			return
		}
	}
}

func (s *SFU) cleanupRoom(roomID string) {
	time.Sleep(5 * time.Minute) // Wait before cleanup

	s.mu.Lock()
	defer s.mu.Unlock()

	room, exists := s.rooms[roomID]
	if !exists {
		return
	}

	room.mu.RLock()
	clientCount := len(room.Clients)
	room.mu.RUnlock()

	if clientCount == 0 {
		delete(s.rooms, roomID)
		log.Printf("Cleaned up empty room %s", roomID)
	}
}

// GetRoomStats returns statistics for monitoring
func (s *SFU) GetRoomStats(roomID string) (clientCount int, trackCount int) {
	s.mu.RLock()
	room, exists := s.rooms[roomID]
	s.mu.RUnlock()

	if !exists {
		return 0, 0
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	return len(room.Clients), len(room.Tracks)
}

// CleanupInactiveRooms removes rooms with no recent activity
func (s *SFU) CleanupInactiveRooms(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for roomID, room := range s.rooms {
		room.mu.RLock()
		isEmpty := len(room.Clients) == 0
		lastActivity := room.LastActivity
		room.mu.RUnlock()

		if isEmpty && now.Sub(lastActivity) > maxAge {
			delete(s.rooms, roomID)
			log.Printf("Cleaned up inactive room %s", roomID)
		}
	}
}
