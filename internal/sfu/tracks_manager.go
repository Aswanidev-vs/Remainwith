package sfu

import (
	"fmt"
	"log"
	"sync"

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
}

// NewTracksManager creates a new tracks manager
func NewTracksManager() *TracksManager {
	return &TracksManager{
		peerManagers: make(map[string]*PeerManager),
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

			// Remove from map
			delete(tm.peerManagers, roomID)
		}
	}()

	return pubTrackEventsCh, nil
}

// Sub subscribes a client to a track
func (tm *TracksManager) Sub(params SubParams, writer transport.TrackLocal, rtcpReader transport.RTCPReader) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pm, ok := tm.peerManagers[params.RoomID]
	if !ok {
		return fmt.Errorf("room not found: %s", params.RoomID)
	}

	return pm.Sub(params.PubClientID, params.TrackID, params.SubClientID, writer, rtcpReader)
}

// Unsub unsubscribes a client from a track
func (tm *TracksManager) Unsub(params SubParams) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pm, ok := tm.peerManagers[params.RoomID]
	if !ok {
		return fmt.Errorf("room not found: %s", params.RoomID)
	}

	return pm.Unsub(params.PubClientID, params.TrackID, params.SubClientID)
}

// GetRoomStats returns statistics for a room
func (tm *TracksManager) GetRoomStats(roomID string) (clientCount int, trackCount int) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	pm, ok := tm.peerManagers[roomID]
	if !ok {
		return 0, 0
	}

	return pm.Size(), 0 // TODO: track count
}

// CleanupInactiveRooms removes rooms with no activity
func (tm *TracksManager) CleanupInactiveRooms() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for roomID, pm := range tm.peerManagers {
		if pm.Size() == 0 {
			log.Printf("TracksManager: Cleaning up inactive room %s", roomID)

			// Close peer manager
			<-pm.Close()

			// Remove from map
			delete(tm.peerManagers, roomID)
		}
	}
}
