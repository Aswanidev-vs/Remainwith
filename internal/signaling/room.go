package signaling

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ParticipantRole defines the role of a participant in the room
type ParticipantRole string

const (
	RoleMember    ParticipantRole = "member"
	RoleHost      ParticipantRole = "host"
	RoleModerator ParticipantRole = "moderator"
)

// Participant represents a participant in a room
type Participant struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Role        ParticipantRole `json:"role"`
	JoinedAt    time.Time       `json:"joined_at"`
	IsOnline    bool            `json:"is_online"`
	IsMuted     bool            `json:"is_muted"`
	CameraOn    bool            `json:"camera_on"`
	Permissions map[string]bool `json:"permissions"`
}

// Room represents a video call room
type Room struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	HostID          string                  `json:"host_id"`
	HostName        string                  `json:"host_name"`
	CreatedAt       time.Time               `json:"created_at"`
	LastActivity    time.Time               `json:"last_activity"`
	Metadata        map[string]interface{}  `json:"metadata"`
	Participants    map[string]*Participant `json:"participants"`
	MaxParticipants int                     `json:"max_participants"`
	IsActive        bool                    `json:"is_active"`
	SessionTimeout  time.Duration           `json:"session_timeout"`
	mu              sync.RWMutex
}

// NewRoom creates a new room
func NewRoom(name, hostID, hostName string) *Room {
	return &Room{
		ID:              uuid.New().String(),
		Name:            name,
		HostID:          hostID,
		HostName:        hostName,
		CreatedAt:       time.Now(),
		Metadata:        make(map[string]interface{}),
		Participants:    make(map[string]*Participant),
		MaxParticipants: 10, // Default max
		IsActive:        true,
	}
}

// AddParticipant adds a participant to the room
func (r *Room) AddParticipant(id, name string, role ParticipantRole) *Participant {
	r.mu.Lock()
	defer r.mu.Unlock()

	participant := &Participant{
		ID:          id,
		Name:        name,
		Role:        role,
		JoinedAt:    time.Now(),
		IsOnline:    true,
		IsMuted:     false,
		CameraOn:    true,
		Permissions: r.getDefaultPermissions(role),
	}

	r.Participants[id] = participant
	return participant
}

// RemoveParticipant removes a participant from the room
func (r *Room) RemoveParticipant(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Participants, id)
}

// GetParticipant gets a participant by ID
func (r *Room) GetParticipant(id string) (*Participant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, exists := r.Participants[id]
	return p, exists
}

// GetParticipants returns all participants
func (r *Room) GetParticipants() map[string]*Participant {
	r.mu.RLock()
	defer r.mu.RUnlock()

	participants := make(map[string]*Participant)
	for k, v := range r.Participants {
		participants[k] = v
	}
	return participants
}

// UpdateParticipant updates participant status
func (r *Room) UpdateParticipant(id string, updates map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, exists := r.Participants[id]; exists {
		if muted, ok := updates["is_muted"].(bool); ok {
			p.IsMuted = muted
		}
		if cameraOn, ok := updates["camera_on"].(bool); ok {
			p.CameraOn = cameraOn
		}
		if online, ok := updates["is_online"].(bool); ok {
			p.IsOnline = online
		}
	}
}

// HasPermission checks if a participant has a specific permission
func (r *Room) HasPermission(participantID, permission string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if p, exists := r.Participants[participantID]; exists {
		if perm, ok := p.Permissions[permission]; ok {
			return perm
		}
	}
	return false
}

// getDefaultPermissions returns default permissions based on role
func (r *Room) getDefaultPermissions(role ParticipantRole) map[string]bool {
	permissions := map[string]bool{
		"can_speak": true,
		"can_see":   true,
	}

	switch role {
	case RoleHost, RoleModerator:
		permissions["can_mute_others"] = true
		permissions["can_kick"] = true
		permissions["can_manage_room"] = true
	}

	return permissions
}

// UpdateActivity updates the last activity timestamp
func (r *Room) UpdateActivity() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.LastActivity = time.Now()
}

// IsExpired checks if the room session has expired
func (r *Room) IsExpired() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return time.Since(r.LastActivity) > r.SessionTimeout
}

// RoomManager manages all rooms
type RoomManager struct {
	rooms map[string]*Room
	mu    sync.RWMutex
}

var (
	roomManager *RoomManager
	once        sync.Once
)

// GetRoomManager returns the singleton room manager
func GetRoomManager() *RoomManager {
	once.Do(func() {
		roomManager = &RoomManager{
			rooms: make(map[string]*Room),
		}
	})
	return roomManager
}

// CreateRoom creates a new room
func (rm *RoomManager) CreateRoom(name, hostID, hostName string) *Room {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	room := NewRoom(name, hostID, hostName)
	rm.rooms[room.ID] = room
	return room
}

// CreateRoomWithID creates a room with a specific ID
func (rm *RoomManager) CreateRoomWithID(roomID, name, hostID, hostName string) *Room {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.rooms[roomID]; exists {
		return nil // Room ID already exists
	}

	now := time.Now()
	room := &Room{
		ID:              roomID,
		Name:            name,
		HostID:          hostID,
		HostName:        hostName,
		CreatedAt:       now,
		LastActivity:    now,
		Metadata:        make(map[string]interface{}),
		Participants:    make(map[string]*Participant),
		MaxParticipants: 10,
		IsActive:        true,
		SessionTimeout:  30 * time.Minute,
	}

	rm.rooms[roomID] = room
	return room
}

// GetRoom gets a room by ID
func (rm *RoomManager) GetRoom(id string) (*Room, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	room, exists := rm.rooms[id]
	return room, exists
}

// GetRoomByName gets a room by name
func (rm *RoomManager) GetRoomByName(name string) (*Room, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for _, room := range rm.rooms {
		if room.Name == name {
			return room, true
		}
	}
	return nil, false
}

// DeleteRoom deletes a room
func (rm *RoomManager) DeleteRoom(id string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.rooms, id)
}

// GetActiveRooms returns all active rooms
func (rm *RoomManager) GetActiveRooms() []*Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var activeRooms []*Room
	for _, room := range rm.rooms {
		if room.IsActive {
			activeRooms = append(activeRooms, room)
		}
	}
	return activeRooms
}

// CleanupInactiveRooms removes rooms that have been inactive for the specified duration
func (rm *RoomManager) CleanupInactiveRooms(maxAge time.Duration) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	for roomID, room := range rm.rooms {
		room.mu.RLock()
		isEmpty := len(room.Participants) == 0
		lastActivity := room.CreatedAt // Use creation time as fallback
		if room.LastActivity.After(room.CreatedAt) {
			lastActivity = room.LastActivity
		}
		room.mu.RUnlock()

		if isEmpty && now.Sub(lastActivity) > maxAge {
			delete(rm.rooms, roomID)
			log.Printf("Cleaned up inactive room %s", roomID)
		}
	}
}
