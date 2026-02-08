package simulcast

import (
	"log"
	"sync"
	"time"
)

// Manager manages simulcast layer selection for all tracks in a room
type Manager struct {
	mu sync.RWMutex

	// Simulcast tracks indexed by trackID
	tracks map[string]*SimulcastTrack

	// Subscriber preferences indexed by subscriberID -> trackID -> preferred layer
	subscriberPreferences map[string]map[string]Layer

	// Global bandwidth limit per subscriber (bps)
	subscriberBandwidth map[string]uint32

	// Cleanup ticker
	cleanupTicker *time.Ticker
	done          chan struct{}
}

// NewManager creates a new simulcast manager
func NewManager() *Manager {
	m := &Manager{
		tracks:                make(map[string]*SimulcastTrack),
		subscriberPreferences: make(map[string]map[string]Layer),
		subscriberBandwidth:   make(map[string]uint32),
		cleanupTicker:         time.NewTicker(30 * time.Second),
		done:                  make(chan struct{}),
	}

	go m.cleanupLoop()

	return m
}

// cleanupLoop periodically cleans up stale resources
func (m *Manager) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanup()
		case <-m.done:
			return
		}
	}
}

// cleanup removes stale tracks and subscribers
func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove inactive tracks
	for trackID, track := range m.tracks {
		activeLayers := track.GetActiveLayers()
		if len(activeLayers) == 0 {
			delete(m.tracks, trackID)
			log.Printf("SimulcastManager: Removed inactive track %s", trackID)
		}
	}
}

// AddTrack adds a simulcast track to the manager
func (m *Manager) AddTrack(trackID, clientID string) *SimulcastTrack {
	m.mu.Lock()
	defer m.mu.Unlock()

	track := NewSimulcastTrack(trackID, clientID)
	m.tracks[trackID] = track

	log.Printf("SimulcastManager: Added track %s for client %s", trackID, clientID)

	return track
}

// RemoveTrack removes a simulcast track
func (m *Manager) RemoveTrack(trackID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tracks, trackID)

	// Clean up subscriber preferences for this track
	for subscriberID, preferences := range m.subscriberPreferences {
		delete(preferences, trackID)
		if len(preferences) == 0 {
			delete(m.subscriberPreferences, subscriberID)
		}
	}

	log.Printf("SimulcastManager: Removed track %s", trackID)
}

// GetTrack returns a simulcast track by ID
func (m *Manager) GetTrack(trackID string) (*SimulcastTrack, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	track, ok := m.tracks[trackID]
	return track, ok
}

// SetSubscriberBandwidth sets the bandwidth limit for a subscriber
func (m *Manager) SetSubscriberBandwidth(subscriberID string, bandwidth uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.subscriberBandwidth[subscriberID] = bandwidth

	log.Printf("SimulcastManager: Set bandwidth for subscriber %s to %d bps", subscriberID, bandwidth)
}

// GetSubscriberBandwidth returns the bandwidth limit for a subscriber
func (m *Manager) GetSubscriberBandwidth(subscriberID string) uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.subscriberBandwidth[subscriberID]
}

// SetSubscriberLayerPreference sets a subscriber's preferred layer for a track
func (m *Manager) SetSubscriberLayerPreference(subscriberID, trackID string, layer Layer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.subscriberPreferences[subscriberID] == nil {
		m.subscriberPreferences[subscriberID] = make(map[string]Layer)
	}

	m.subscriberPreferences[subscriberID][trackID] = layer

	log.Printf("SimulcastManager: Subscriber %s prefers layer %s for track %s", subscriberID, layer.String(), trackID)
}

// GetSubscriberLayerPreference returns a subscriber's preferred layer for a track
func (m *Manager) GetSubscriberLayerPreference(subscriberID, trackID string) (Layer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	preferences, ok := m.subscriberPreferences[subscriberID]
	if !ok {
		return LayerHigh, false
	}

	layer, ok := preferences[trackID]
	return layer, ok
}

// SelectOptimalLayer selects the optimal layer for a subscriber viewing a track
// based on available bandwidth and subscriber preferences
func (m *Manager) SelectOptimalLayer(subscriberID, trackID string) Layer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	track, ok := m.tracks[trackID]
	if !ok {
		return LayerHigh // Default to high if track not found
	}

	// Get subscriber's bandwidth limit
	bandwidth := m.subscriberBandwidth[subscriberID]
	if bandwidth == 0 {
		bandwidth = 10000000 // Default to 10 Mbps if not set
	}

	// Check if subscriber has a preference
	if preferredLayer, ok := m.subscriberPreferences[subscriberID][trackID]; ok {
		// Verify the preferred layer is within bandwidth constraints
		if info, ok := track.GetLayer(preferredLayer); ok && info.Active {
			if info.Bitrate <= bandwidth {
				return preferredLayer
			}
		}
	}

	// Use automatic layer selection based on bandwidth
	return track.GetOptimalLayer(bandwidth)
}

// UpdateLayerBitrate updates the bitrate for a specific layer
func (m *Manager) UpdateLayerBitrate(trackID string, layer Layer, bitrate uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if track, ok := m.tracks[trackID]; ok {
		track.UpdateBitrate(layer, bitrate)
	}
}

// GetAllTracks returns all managed simulcast tracks
func (m *Manager) GetAllTracks() map[string]*SimulcastTrack {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	tracks := make(map[string]*SimulcastTrack)
	for k, v := range m.tracks {
		tracks[k] = v
	}

	return tracks
}

// GetSubscriberStats returns statistics for a subscriber
func (m *Manager) GetSubscriberStats(subscriberID string) map[string]struct {
	CurrentLayer Layer
	Bitrate      uint32
} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]struct {
		CurrentLayer Layer
		Bitrate      uint32
	})

	for trackID, track := range m.tracks {
		layer := m.SelectOptimalLayer(subscriberID, trackID)
		if info, ok := track.GetLayer(layer); ok {
			stats[trackID] = struct {
				CurrentLayer Layer
				Bitrate      uint32
			}{
				CurrentLayer: layer,
				Bitrate:      info.Bitrate,
			}
		}
	}

	return stats
}

// Close closes the simulcast manager
func (m *Manager) Close() {
	close(m.done)
	m.cleanupTicker.Stop()
}
