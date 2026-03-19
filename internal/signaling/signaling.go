package signaling

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	signalingWriteWait  = 10 * time.Second
	signalingPongWait   = 30 * time.Second
	signalingPingPeriod = 10 * time.Second
	signalingLeaveGrace = 5 * time.Second
)

// SignalMessage represents a signaling message
type SignalMessage struct {
	Type         string      `json:"type"`
	RoomID       string      `json:"room_id,omitempty"`
	UserID       string      `json:"user_id,omitempty"`
	TargetUserID string      `json:"target_user_id,omitempty"`
	Data         interface{} `json:"data,omitempty"`
}

// PinMessage represents pin/unpin message data
type PinMessage struct {
	PinnedUserID string `json:"pinned_user_id"`
	IsPinned     bool   `json:"is_pinned"`
}

// ActiveSpeakerInfo represents active speaker information
type ActiveSpeakerInfo struct {
	UserID     string  `json:"user_id"`
	AudioLevel float64 `json:"audio_level"`
	IsSpeaking bool    `json:"is_speaking"`
}

type signalingConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *signalingConn) WriteJSON(msg interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(signalingWriteWait)); err != nil {
		return err
	}

	return c.conn.WriteJSON(msg)
}

func (c *signalingConn) WritePing() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(signalingWriteWait))
}

func (c *signalingConn) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Close()
}

func signalingSessionKey(roomID, userID string) string {
	return roomID + ":" + userID
}

// SignalingServer handles WebRTC signaling
type SignalingServer struct {
	upgrader      websocket.Upgrader
	clients       map[string]*signalingConn            // userID -> conn
	rooms         map[string]map[string]*signalingConn // roomID -> userID -> conn
	offlineTimers map[string]*time.Timer
	mu            sync.RWMutex
}

// NewSignalingServer creates a new signaling server
func NewSignalingServer() *SignalingServer {
	s := &SignalingServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
		clients:       make(map[string]*signalingConn),
		rooms:         make(map[string]map[string]*signalingConn),
		offlineTimers: make(map[string]*time.Timer),
	}

	return s
}

// HandleConnection handles WebSocket connections
func (s *SignalingServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	roomID := r.URL.Query().Get("room_id")

	if userID == "" || roomID == "" {
		http.Error(w, "user_id and room_id required", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	clientConn := &signalingConn{conn: conn}

	if err := conn.SetReadDeadline(time.Now().Add(signalingPongWait)); err != nil {
		log.Printf("Failed to set initial signaling read deadline: %v", err)
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(signalingPongWait))
	})

	sessionKey := signalingSessionKey(roomID, userID)

	s.mu.Lock()
	if timer := s.offlineTimers[sessionKey]; timer != nil {
		timer.Stop()
		delete(s.offlineTimers, sessionKey)
	}
	if existing := s.clients[sessionKey]; existing != nil {
		_ = existing.Close()
	}
	if existingRoomClients := s.rooms[roomID]; existingRoomClients != nil {
		if existing := existingRoomClients[userID]; existing != nil {
			_ = existing.Close()
		}
	}
	s.clients[sessionKey] = clientConn
	if s.rooms[roomID] == nil {
		s.rooms[roomID] = make(map[string]*signalingConn)
	}
	s.rooms[roomID][userID] = clientConn
	s.mu.Unlock()

	log.Printf("User %s joined room %s", userID, roomID)

	// Handle messages
	go s.handleMessages(clientConn, userID, roomID)
}

// handleMessages processes incoming WebSocket messages
func (s *SignalingServer) handleMessages(conn *signalingConn, userID, roomID string) {
	done := make(chan struct{})
	pingTicker := time.NewTicker(signalingPingPeriod)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				if err := conn.WritePing(); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	defer func() {
		close(done)
		sessionKey := signalingSessionKey(roomID, userID)
		s.mu.Lock()
		if current, exists := s.clients[sessionKey]; exists && current == conn {
			delete(s.clients, sessionKey)
		}
		if roomClients, exists := s.rooms[roomID]; exists {
			if current, ok := roomClients[userID]; ok && current == conn {
				delete(roomClients, userID)
			}
			if len(roomClients) == 0 {
				delete(s.rooms, roomID)
			}
		}
		s.mu.Unlock()

		s.scheduleOfflineTransition(roomID, userID, conn)

		_ = conn.Close()
		log.Printf("User %s left room %s", userID, roomID)
	}()

	for {
		var msg SignalMessage
		err := conn.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("User %s signaling socket closed unexpectedly in room %s: %v", userID, roomID, err)
			} else {
				log.Printf("User %s signaling socket closed in room %s: %v", userID, roomID, err)
			}
			break
		}

		msg.UserID = userID
		msg.RoomID = roomID

		s.handleSignalMessage(msg)
	}
}

func (s *SignalingServer) scheduleOfflineTransition(roomID, userID string, conn *signalingConn) {
	sessionKey := signalingSessionKey(roomID, userID)

	s.mu.Lock()
	if timer := s.offlineTimers[sessionKey]; timer != nil {
		timer.Stop()
	}

	s.offlineTimers[sessionKey] = time.AfterFunc(signalingLeaveGrace, func() {
		s.mu.Lock()
		currentConn := s.clients[sessionKey]
		if currentConn != nil && currentConn != conn {
			delete(s.offlineTimers, sessionKey)
			s.mu.Unlock()
			return
		}
		delete(s.offlineTimers, sessionKey)
		s.mu.Unlock()

		rm := GetRoomManager()
		if room, exists := rm.GetRoom(roomID); exists {
			if participant, ok := room.GetParticipant(userID); ok {
				if !participant.IsOnline {
					return
				}
				room.UpdateParticipant(userID, map[string]interface{}{"is_online": false})
				s.broadcastToRoom(SignalMessage{
					Type:   "participant_left",
					RoomID: roomID,
					UserID: userID,
					Data:   participant,
				}, userID)
			}
		}
	})
	s.mu.Unlock()
}

// handleSignalMessage processes signaling messages
func (s *SignalingServer) handleSignalMessage(msg SignalMessage) {
	switch msg.Type {
	case "join":
		s.handleJoin(msg)
	case "leave":
		s.handleLeave(msg)
	case "offer":
		s.broadcastToRoom(msg, msg.UserID)
	case "answer":
		s.broadcastToRoom(msg, msg.UserID)
	case "candidate":
		s.broadcastToRoom(msg, msg.UserID)
	case "mute":
		s.handleMute(msg)
	case "camera_toggle":
		s.handleCameraToggle(msg)
	case "pin":
		s.handlePin(msg)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

func (s *SignalingServer) handleLeave(msg SignalMessage) {
	rm := GetRoomManager()
	room, exists := rm.GetRoom(msg.RoomID)
	if !exists {
		return
	}

	room.UpdateParticipant(msg.UserID, map[string]interface{}{"is_online": false})
	if participant, ok := room.GetParticipant(msg.UserID); ok {
		s.broadcastToRoom(SignalMessage{
			Type:   "participant_left",
			RoomID: msg.RoomID,
			UserID: msg.UserID,
			Data:   participant,
		}, msg.UserID)
	}
}

// handleJoin handles join messages
func (s *SignalingServer) handleJoin(msg SignalMessage) {
	rm := GetRoomManager()
	room, exists := rm.GetRoom(msg.RoomID)
	if !exists {
		log.Printf("Room %s not found", msg.RoomID)
		return
	}

	// Update participant status
	room.UpdateParticipant(msg.UserID, map[string]interface{}{"is_online": true})

	participants := room.GetParticipants()
	existingParticipants := make([]*Participant, 0, len(participants))
	for _, participant := range participants {
		if participant.ID != msg.UserID && participant.IsOnline {
			existingParticipants = append(existingParticipants, participant)
		}
	}

	s.broadcastToRoom(SignalMessage{
		Type:         "existing_participants",
		RoomID:       msg.RoomID,
		TargetUserID: msg.UserID,
		Data:         existingParticipants,
	}, "")

	// Notify others in room
	if participant, exists := room.GetParticipant(msg.UserID); exists {
		s.broadcastToRoom(SignalMessage{
			Type:   "participant_joined",
			RoomID: msg.RoomID,
			UserID: msg.UserID,
			Data:   participant,
		}, "")
	}
}

// broadcastToRoom sends a message to all clients in a room except the sender, or to specific user
func (s *SignalingServer) broadcastToRoom(msg SignalMessage, excludeUserID string) {
	s.mu.RLock()
	roomClients, exists := s.rooms[msg.RoomID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	if msg.TargetUserID != "" {
		// Send to specific user
		if conn, exists := roomClients[msg.TargetUserID]; exists {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("Error sending message to %s: %v", msg.TargetUserID, err)
			}
		}
		return
	}

	// Send to all except sender
	for userID, conn := range roomClients {
		if userID != excludeUserID {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("Error sending message to %s: %v", userID, err)
			}
		}
	}
}

// handleMute handles mute/unmute messages
func (s *SignalingServer) handleMute(msg SignalMessage) {
	rm := GetRoomManager()
	room, exists := rm.GetRoom(msg.RoomID)
	if !exists {
		return
	}

	muted, ok := msg.Data.(bool)
	if !ok {
		return
	}

	room.UpdateParticipant(msg.UserID, map[string]interface{}{"is_muted": muted})

	// Broadcast mute status
	s.broadcastToRoom(SignalMessage{
		Type:   "mute_status",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
		Data:   muted,
	}, "")
}

// handleCameraToggle handles camera on/off messages
func (s *SignalingServer) handleCameraToggle(msg SignalMessage) {
	rm := GetRoomManager()
	room, exists := rm.GetRoom(msg.RoomID)
	if !exists {
		return
	}

	cameraOn, ok := msg.Data.(bool)
	if !ok {
		return
	}

	room.UpdateParticipant(msg.UserID, map[string]interface{}{"camera_on": cameraOn})

	// Broadcast camera status
	s.broadcastToRoom(SignalMessage{
		Type:   "camera_status",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
		Data:   cameraOn,
	}, "")
}

// handlePin handles pin/unpin messages
func (s *SignalingServer) handlePin(msg SignalMessage) {
	pinData, ok := msg.Data.(map[string]interface{})
	if !ok {
		return
	}

	pinnedUserID, hasPinnedUserID := pinData["pinned_user_id"].(string)
	isPinned, hasIsPinned := pinData["is_pinned"].(bool)

	if !hasPinnedUserID || !hasIsPinned {
		return
	}

	// Broadcast pin status to all participants in room
	s.broadcastToRoom(SignalMessage{
		Type:   "pin",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
		Data: PinMessage{
			PinnedUserID: pinnedUserID,
			IsPinned:     isPinned,
		},
	}, "")
}

// handleAudioLevel handles audio level updates for active speaker detection
func (s *SignalingServer) handleAudioLevel(msg SignalMessage) {
	audioLevel, ok := msg.Data.(float64)
	if !ok {
		return
	}

	isSpeaking := audioLevel > 0.1 // Threshold for speaking detection

	activeSpeaker := ActiveSpeakerInfo{
		UserID:     msg.UserID,
		AudioLevel: audioLevel,
		IsSpeaking: isSpeaking,
	}

	// Broadcast active speaker info to room
	s.broadcastToRoom(SignalMessage{
		Type:   "active_speaker",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
		Data:   activeSpeaker,
	}, "")
}
