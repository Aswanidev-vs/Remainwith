package sfu

import (
	"fmt"
	"log"
	"sync"
	"time"

	"Remainwith/internal/sfu/pubsub"
	"Remainwith/internal/transport"
)

// SubParams represents parameters for subscribing to a track
type SubParams struct {
	PubClientID string
	RoomID      string
	TrackID     string
	SubClientID string
}

// TracksManager manages tracks across all rooms
type TracksManager struct {
	mu           sync.RWMutex
	peerManagers map[string]*PeerManager // roomID -> PeerManager
	roomActivity map[string]time.Time    // roomID -> last activity time
}

// NewTracksManager creates a new tracks manager
func NewTracksManager() *TracksManager {
	return &TracksManager{
		peerManagers: make(map[string]*PeerManager),
		roomActivity: make(map[string]time.Time),
	}
}

// Add adds a transport to a room. Creates the room if it doesn't exist.
func (tm *TracksManager) Add(roomID string, tr transport.Transport) (<-chan pubsub.PubTrackEvent, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Get or create peer manager for room
	pm, ok := tm.peerManagers[roomID]
	if !ok {
		log.Printf("TracksManager: Creating peer manager for room %s", roomID)
		pm = NewPeerManager(roomID)
		tm.peerManagers[roomID] = pm
	}

	// Update room activity
	tm.roomActivity[roomID] = time.Now()

	// Add transport to peer manager
	pubTrackEventsCh, err := pm.Add(tr)
	if err != nil {
		return nil, fmt.Errorf("add transport to peer manager: %w", err)
	}

	// Cleanup when transport is done
	go func() {
		<-tr.Done()

		tm.mu.Lock()
		defer tm.mu.Unlock()

		// Update room activity on transport removal
		tm.roomActivity[roomID] = time.Now()

		// Remove transport
		if err := pm.Remove(tr); err != nil {
			log.Printf("TracksManager: Error removing transport: %v", err)
		} else {
			log.Printf("TracksManager: Removed transport for client %s from room %s", tr.ClientID(), roomID)
		}

		// Clean up empty rooms
		if pm.Size() == 0 {
			log.Printf("TracksManager: Cleaning up empty room %s", roomID)

			// Close peer manager
			<-pm.Close()

			// Remove from maps
			delete(tm.peerManagers, roomID)
			delete(tm.roomActivity, roomID)
		}
	}()

	return pubTrackEventsCh, nil
}

// Sub subscribes a client to a track
// Phase 4: Immediate subscription with enhanced error handling
func (tm *TracksManager) Sub(params SubParams, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pm, ok := tm.peerManagers[params.RoomID]
	if !ok {
		return fmt.Errorf("room not found: %s", params.RoomID)
	}

	// Update room activity
	tm.roomActivity[params.RoomID] = time.Now()

	// Phase 4: Immediate subscription - no delay, direct subscription
	err := pm.Sub(params.PubClientID, params.TrackID, params.SubClientID, writer, rtcpReader)
	if err == nil {
		log.Printf("TracksManager: Immediately subscribed %s to track %s from %s in room %s",
			params.SubClientID, params.TrackID, params.PubClientID, params.RoomID)
		return nil
	}

	// If immediate subscription fails, try with short retry
	log.Printf("TracksManager: Immediate sub failed for %s, retrying: %v", params.TrackID, err)
	time.Sleep(5 * time.Millisecond)

	err = pm.Sub(params.PubClientID, params.TrackID, params.SubClientID, writer, rtcpReader)
	if err == nil {
		log.Printf("TracksManager: Subscribed %s to track %s from %s in room %s (retry)",
			params.SubClientID, params.TrackID, params.PubClientID, params.RoomID)
		return nil
	}

	return fmt.Errorf("subscribe failed: %w", err)
}

// SubImmediate provides immediate subscription for auto-subscription scenarios
// Phase 4: Used when a new track is published and should be immediately forwarded
func (tm *TracksManager) SubImmediate(roomID, pubClientID, trackID, subClientID string, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	return tm.Sub(SubParams{
		RoomID:      roomID,
		PubClientID: pubClientID,
		TrackID:     trackID,
		SubClientID: subClientID,
	}, writer, rtcpReader)
}

// Unsub unsubscribes a client from a track
func (tm *TracksManager) Unsub(params SubParams) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pm, ok := tm.peerManagers[params.RoomID]
	if !ok {
		return fmt.Errorf("room not found: %s", params.RoomID)
	}

	// Update room activity
	tm.roomActivity[params.RoomID] = time.Now()

	err := pm.Unsub(params.PubClientID, params.TrackID, params.SubClientID)
	if err != nil {
		return fmt.Errorf("unsubscribe failed: %w", err)
	}

	log.Printf("TracksManager: Unsubscribed %s from track %s from %s in room %s",
		params.SubClientID, params.TrackID, params.PubClientID, params.RoomID)
	return nil
}

// GetRoomStats returns statistics for a room
func (tm *TracksManager) GetRoomStats(roomID string) (clientCount int, trackCount int) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	pm, ok := tm.peerManagers[roomID]
	if !ok {
		return 0, 0
	}

	// Get track count from pubsub
	trackCount = 0
	if pm.pubsub != nil {
		trackCount = len(pm.pubsub.Tracks())
	}

	return pm.Size(), trackCount
}

// GetRoomActivity returns the last activity time for a room
func (tm *TracksManager) GetRoomActivity(roomID string) (time.Time, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	lastActivity, ok := tm.roomActivity[roomID]
	return lastActivity, ok
}

// CleanupInactiveRooms removes rooms with no activity
// Phase 4: Enhanced cleanup with immediate removal of disconnected peers
func (tm *TracksManager) CleanupInactiveRooms() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)            // 5 minutes of inactivity
	aggressiveCutoff := time.Now().Add(-30 * time.Second) // 30 seconds for empty rooms

	for roomID, pm := range tm.peerManagers {
		// Phase 4: Aggressive cleanup for empty rooms
		if pm.Size() == 0 {
			if lastActivity, ok := tm.roomActivity[roomID]; ok && lastActivity.Before(aggressiveCutoff) {
				log.Printf("TracksManager: Cleaning up empty room %s (inactive for 30s)", roomID)
				tm.cleanupRoom(roomID, pm)
			}
			continue
		}

		// Check if room has been inactive
		if lastActivity, ok := tm.roomActivity[roomID]; ok && lastActivity.Before(cutoff) {
			log.Printf("TracksManager: Cleaning up inactive room %s (last activity: %v)", roomID, lastActivity)
			tm.cleanupRoom(roomID, pm)
		}
	}
}

// RemoveClient removes a client and all their tracks immediately
// Phase 4: Immediate cleanup on disconnect
func (tm *TracksManager) RemoveClient(roomID, clientID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pm, ok := tm.peerManagers[roomID]
	if !ok {
		return fmt.Errorf("room not found: %s", roomID)
	}

	// Update room activity
	tm.roomActivity[roomID] = time.Now()

	// Terminate all tracks for this client
	pm.pubsub.Terminate(clientID)

	log.Printf("TracksManager: Removed client %s from room %s", clientID, roomID)

	// Check if room is now empty
	if pm.Size() == 0 {
		log.Printf("TracksManager: Room %s is now empty, scheduling cleanup", roomID)
		// Room will be cleaned up by CleanupInactiveRooms after 30 seconds
	}

	return nil
}

// cleanupRoom removes a room and cleans up resources
func (tm *TracksManager) cleanupRoom(roomID string, pm *PeerManager) {
	// Close peer manager
	<-pm.Close()

	// Remove from maps
	delete(tm.peerManagers, roomID)
	delete(tm.roomActivity, roomID)
}
