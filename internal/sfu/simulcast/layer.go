package simulcast

import (
	"fmt"
	"sync"
)

// Layer represents a simulcast layer (quality level)
type Layer int

const (
	// LayerLow is the lowest quality layer (small resolution, low bitrate)
	LayerLow Layer = iota
	// LayerMedium is the medium quality layer
	LayerMedium
	// LayerHigh is the highest quality layer (full resolution, high bitrate)
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
	Layer      Layer
	SSRC       uint32
	Bitrate    uint32 // Target bitrate in bps
	Width      uint16
	Height     uint16
	FrameRate  uint8
	TemporalID uint8 // Temporal layer ID for SVC
	Active     bool
}

// SimulcastTrack manages simulcast layers for a single track
type SimulcastTrack struct {
	mu sync.RWMutex

	trackID      string
	clientID     string
	layers       map[Layer]*LayerInfo
	currentLayer Layer

	// Bitrate tracking
	bitrateEstimator *BitrateEstimator
}

// NewSimulcastTrack creates a new simulcast track manager
func NewSimulcastTrack(trackID, clientID string) *SimulcastTrack {
	return &SimulcastTrack{
		trackID:          trackID,
		clientID:         clientID,
		layers:           make(map[Layer]*LayerInfo),
		currentLayer:     LayerHigh, // Default to high quality
		bitrateEstimator: NewBitrateEstimator(),
	}
}

// AddLayer adds a simulcast layer
func (st *SimulcastTrack) AddLayer(info *LayerInfo) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if info.Layer < LayerLow || info.Layer >= LayerMax {
		return fmt.Errorf("invalid layer: %d", info.Layer)
	}

	st.layers[info.Layer] = info
	return nil
}

// RemoveLayer removes a simulcast layer
func (st *SimulcastTrack) RemoveLayer(layer Layer) {
	st.mu.Lock()
	defer st.mu.Unlock()

	delete(st.layers, layer)
}

// GetLayer returns information about a specific layer
func (st *SimulcastTrack) GetLayer(layer Layer) (*LayerInfo, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	info, ok := st.layers[layer]
	return info, ok
}

// GetCurrentLayer returns the currently selected layer
func (st *SimulcastTrack) GetCurrentLayer() Layer {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return st.currentLayer
}

// SetLayer sets the current layer based on available bandwidth
func (st *SimulcastTrack) SetLayer(availableBandwidth uint32) Layer {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Find the best layer that fits within available bandwidth
	selectedLayer := LayerLow

	for layer := LayerHigh; layer >= LayerLow; layer-- {
		if info, ok := st.layers[layer]; ok && info.Active {
			if info.Bitrate <= availableBandwidth {
				selectedLayer = layer
				break
			}
		}
	}

	st.currentLayer = selectedLayer
	return selectedLayer
}

// GetOptimalLayer returns the optimal layer for a given bandwidth constraint
func (st *SimulcastTrack) GetOptimalLayer(maxBandwidth uint32) Layer {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// Start from high and go down until we find a layer that fits
	for layer := LayerHigh; layer >= LayerLow; layer-- {
		if info, ok := st.layers[layer]; ok && info.Active {
			if info.Bitrate <= maxBandwidth {
				return layer
			}
		}
	}

	// Fallback to lowest layer
	return LayerLow
}

// GetActiveLayers returns all active layers
func (st *SimulcastTrack) GetActiveLayers() []*LayerInfo {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var active []*LayerInfo
	for _, info := range st.layers {
		if info.Active {
			active = append(active, info)
		}
	}

	return active
}

// UpdateBitrate updates the bitrate for a layer
func (st *SimulcastTrack) UpdateBitrate(layer Layer, bitrate uint32) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if info, ok := st.layers[layer]; ok {
		info.Bitrate = bitrate
	}
}

// GetBitrateEstimator returns the bitrate estimator for this track
func (st *SimulcastTrack) GetBitrateEstimator() *BitrateEstimator {
	return st.bitrateEstimator
}

// BitrateEstimator estimates available bandwidth
type BitrateEstimator struct {
	mu sync.RWMutex

	// Estimated bitrate in bps
	estimatedBitrate uint32

	// History of bitrate measurements
	history    []uint32
	maxHistory int

	// Minimum and maximum observed bitrates
	minBitrate uint32
	maxBitrate uint32
}

// NewBitrateEstimator creates a new bitrate estimator
func NewBitrateEstimator() *BitrateEstimator {
	return &BitrateEstimator{
		history:    make([]uint32, 0, 10),
		maxHistory: 10,
		minBitrate: 100000,   // 100 kbps minimum
		maxBitrate: 10000000, // 10 Mbps maximum
	}
}

// Feed adds a bitrate measurement
func (be *BitrateEstimator) Feed(bitrate uint32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	// Add to history
	be.history = append(be.history, bitrate)
	if len(be.history) > be.maxHistory {
		be.history = be.history[1:]
	}

	// Calculate estimated bitrate using exponential moving average
	if len(be.history) == 1 {
		be.estimatedBitrate = bitrate
	} else {
		// EMA with alpha = 0.3
		be.estimatedBitrate = uint32(0.3*float64(bitrate) + 0.7*float64(be.estimatedBitrate))
	}

	// Update min/max
	if bitrate < be.minBitrate {
		be.minBitrate = bitrate
	}
	if bitrate > be.maxBitrate {
		be.maxBitrate = bitrate
	}
}

// GetEstimatedBitrate returns the current estimated bitrate
func (be *BitrateEstimator) GetEstimatedBitrate() uint32 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	return be.estimatedBitrate
}

// GetMinBitrate returns the minimum observed bitrate
func (be *BitrateEstimator) GetMinBitrate() uint32 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	return be.minBitrate
}

// GetMaxBitrate returns the maximum observed bitrate
func (be *BitrateEstimator) GetMaxBitrate() uint32 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	return be.maxBitrate
}

// GetAverageBitrate returns the average of recent measurements
func (be *BitrateEstimator) GetAverageBitrate() uint32 {
	be.mu.RLock()
	defer be.mu.RUnlock()

	if len(be.history) == 0 {
		return be.estimatedBitrate
	}

	var sum uint32
	for _, b := range be.history {
		sum += b
	}

	return sum / uint32(len(be.history))
}
