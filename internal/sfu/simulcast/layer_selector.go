// Package simulcast provides simulcast layer selection for adaptive bitrate streaming
package simulcast

import (
	"log"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// Layer represents a simulcast layer (quality level)
type Layer int

const (
	// LayerLow is the lowest quality layer
	LayerLow Layer = iota
	// LayerMedium is the medium quality layer
	LayerMedium
	// LayerHigh is the highest quality layer
	LayerHigh
	// LayerMax is the number of layers
	LayerMax
)

// String returns the string representation of a layer
func (l Layer) String() string {
	switch l {
	case LayerLow:
		return "low"
	case LayerMedium:
		return "medium"
	case LayerHigh:
		return "high"
	default:
		return "unknown"
	}
}

// LayerInfo contains information about a simulcast layer
type LayerInfo struct {
	Layer       Layer
	SSRC        uint32
	Bitrate     uint32 // Estimated bitrate in bps
	Resolution  string // e.g., "640x360"
	FrameRate   uint8
	Active      bool
	LastPacket  time.Time
	PacketCount uint32
	ByteCount   uint64
}

// LayerSelector manages simulcast layer selection
type LayerSelector struct {
	mu sync.RWMutex

	// Available layers indexed by SSRC
	layers map[uint32]*LayerInfo

	// Current selected layer for each subscriber
	selectedLayer map[string]Layer

	// Target bitrate for each subscriber (bps)
	targetBitrate map[string]uint32

	// Layer switching hysteresis to prevent oscillation
	lastSwitchTime map[string]time.Time
	switchCooldown time.Duration

	// Configuration
	minSwitchInterval time.Duration
}

// Config contains layer selector configuration
type Config struct {
	MinSwitchInterval time.Duration
	SwitchCooldown    time.Duration
}

// DefaultConfig returns default layer selector configuration
func DefaultConfig() Config {
	return Config{
		MinSwitchInterval: 2 * time.Second,        // Minimum 2 seconds between switches
		SwitchCooldown:    500 * time.Millisecond, // 500ms cooldown after switch
	}
}

// NewLayerSelector creates a new layer selector
func NewLayerSelector(config Config) *LayerSelector {
	return &LayerSelector{
		layers:            make(map[uint32]*LayerInfo),
		selectedLayer:     make(map[string]Layer),
		targetBitrate:     make(map[string]uint32),
		lastSwitchTime:    make(map[string]time.Time),
		switchCooldown:    config.SwitchCooldown,
		minSwitchInterval: config.MinSwitchInterval,
	}
}

// AddLayer adds a simulcast layer
func (ls *LayerSelector) AddLayer(ssrc uint32, layer Layer, resolution string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.layers[ssrc] = &LayerInfo{
		Layer:      layer,
		SSRC:       ssrc,
		Resolution: resolution,
		Active:     true,
		LastPacket: time.Now(),
	}

	log.Printf("LayerSelector: Added layer %s (SSRC: %d, resolution: %s)", layer, ssrc, resolution)
}

// RemoveLayer removes a simulcast layer
func (ls *LayerSelector) RemoveLayer(ssrc uint32) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	delete(ls.layers, ssrc)
}

// UpdateLayerStats updates layer statistics from incoming packets
func (ls *LayerSelector) UpdateLayerStats(ssrc uint32, packet *rtp.Packet, arrivalTime time.Time) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	layer, ok := ls.layers[ssrc]
	if !ok {
		return
	}

	layer.LastPacket = arrivalTime
	layer.PacketCount++
	layer.ByteCount += uint64(len(packet.Payload))

	// Calculate bitrate (simple moving average over 1 second)
	// In production, use a more sophisticated algorithm
	if layer.PacketCount%30 == 0 { // Update every ~30 packets
		// Estimate based on packet size and expected frame rate
		// This is a simplified calculation
		estimatedBps := uint32(layer.ByteCount * 8 / 10) // Rough 100ms window
		layer.Bitrate = estimatedBps
	}
}

// SelectLayer selects the best layer for a subscriber based on target bitrate
func (ls *LayerSelector) SelectLayer(subscriberID string, targetBitrate uint32) Layer {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// Check cooldown
	if lastSwitch, ok := ls.lastSwitchTime[subscriberID]; ok {
		if time.Since(lastSwitch) < ls.minSwitchInterval {
			// Return current selection during cooldown
			if current, ok := ls.selectedLayer[subscriberID]; ok {
				return current
			}
		}
	}

	// Store target bitrate
	ls.targetBitrate[subscriberID] = targetBitrate

	// Find best layer
	var selected Layer = LayerLow
	var bestScore float64

	for _, layer := range ls.layers {
		if !layer.Active {
			continue
		}

		// Calculate score based on:
		// 1. Bitrate fit (how well it matches target)
		// 2. Layer quality (prefer higher if bandwidth allows)
		bitrateFit := float64(1.0)
		if layer.Bitrate > 0 && targetBitrate > 0 {
			if layer.Bitrate <= targetBitrate {
				// Layer fits within target - good
				bitrateFit = float64(layer.Bitrate) / float64(targetBitrate)
			} else {
				// Layer exceeds target - penalize
				bitrateFit = float64(targetBitrate) / float64(layer.Bitrate) * 0.5
			}
		}

		// Quality bonus (higher layers get bonus if they fit)
		qualityBonus := float64(layer.Layer) * 0.1

		score := bitrateFit + qualityBonus

		if score > bestScore {
			bestScore = score
			selected = layer.Layer
		}
	}

	// Check if we should switch
	current, hasCurrent := ls.selectedLayer[subscriberID]
	if !hasCurrent || current != selected {
		// Apply cooldown for downward switches (to prevent quality drops)
		if hasCurrent && selected < current {
			if lastSwitch, ok := ls.lastSwitchTime[subscriberID]; ok {
				if time.Since(lastSwitch) < ls.switchCooldown {
					return current // Keep current layer during cooldown
				}
			}
		}

		ls.selectedLayer[subscriberID] = selected
		ls.lastSwitchTime[subscriberID] = time.Now()

		log.Printf("LayerSelector: Subscriber %s switched from %s to %s (target: %d bps)",
			subscriberID, current, selected, targetBitrate)
	}

	return selected
}

// GetSelectedLayer returns the currently selected layer for a subscriber
func (ls *LayerSelector) GetSelectedLayer(subscriberID string) (Layer, bool) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	layer, ok := ls.selectedLayer[subscriberID]
	return layer, ok
}

// GetLayerSSRC returns the SSRC for a specific layer
func (ls *LayerSelector) GetLayerSSRC(layer Layer) (uint32, bool) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	for _, info := range ls.layers {
		if info.Layer == layer && info.Active {
			return info.SSRC, true
		}
	}

	return 0, false
}

// GetLayerInfo returns information about all layers
func (ls *LayerSelector) GetLayerInfo() []LayerInfo {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	var infos []LayerInfo
	for _, layer := range ls.layers {
		infos = append(infos, *layer)
	}

	return infos
}

// RemoveSubscriber removes a subscriber from tracking
func (ls *LayerSelector) RemoveSubscriber(subscriberID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	delete(ls.selectedLayer, subscriberID)
	delete(ls.targetBitrate, subscriberID)
	delete(ls.lastSwitchTime, subscriberID)
}

// GetSubscriberStats returns statistics for a subscriber
func (ls *LayerSelector) GetSubscriberStats(subscriberID string) (Layer, uint32) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	layer, _ := ls.selectedLayer[subscriberID]
	bitrate, _ := ls.targetBitrate[subscriberID]

	return layer, bitrate
}

// PauseLayer pauses a layer (temporarily disable)
func (ls *LayerSelector) PauseLayer(layer Layer) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	for _, info := range ls.layers {
		if info.Layer == layer {
			info.Active = false
			log.Printf("LayerSelector: Paused layer %s", layer)
		}
	}
}

// ResumeLayer resumes a paused layer
func (ls *LayerSelector) ResumeLayer(layer Layer) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	for _, info := range ls.layers {
		if info.Layer == layer {
			info.Active = true
			log.Printf("LayerSelector: Resumed layer %s", layer)
		}
	}
}

// Close closes the layer selector
func (ls *LayerSelector) Close() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.layers = make(map[uint32]*LayerInfo)
	ls.selectedLayer = make(map[string]Layer)
	ls.targetBitrate = make(map[string]uint32)
	ls.lastSwitchTime = make(map[string]time.Time)
}
