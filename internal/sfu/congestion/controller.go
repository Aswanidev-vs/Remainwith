// Package congestion provides congestion control for WebRTC SFU
package congestion

import (
	"log"
	"sync"
	"time"
)

// Controller manages congestion control for a subscriber
type Controller struct {
	mu sync.RWMutex

	// Current bitrate estimate (bps)
	currentBitrate uint32

	// Target bitrate (bps)
	targetBitrate uint32

	// Minimum and maximum bitrates
	minBitrate uint32
	maxBitrate uint32

	// Congestion state
	state State

	// RTT tracking
	rttEstimator *RTTEstimator

	// Loss rate tracking
	lossEstimator *LossEstimator

	// GCC (Google Congestion Control) algorithm
	gcc *GCC

	// Callbacks
	onTargetBitrateChange func(uint32)

	// Control
	closed bool
}

// State represents the congestion control state
type State int

const (
	// StateStable indicates normal operation
	StateStable State = iota
	// StateDecrease indicates congestion detected, decreasing bitrate
	StateDecrease
	// StateHold indicates holding bitrate after decrease
	StateHold
	// StateIncrease indicates increasing bitrate after recovery
	StateIncrease
)

// String returns the string representation of a state
func (s State) String() string {
	switch s {
	case StateStable:
		return "stable"
	case StateDecrease:
		return "decrease"
	case StateHold:
		return "hold"
	case StateIncrease:
		return "increase"
	default:
		return "unknown"
	}
}

// Config contains congestion controller configuration
type Config struct {
	MinBitrate        uint32
	MaxBitrate        uint32
	InitialBitrate    uint32
	BitrateChangeStep uint32 // How much to change bitrate per adjustment
}

// DefaultConfig returns default congestion controller configuration
func DefaultConfig() Config {
	return Config{
		MinBitrate:        50000,   // 50 kbps minimum
		MaxBitrate:        4000000, // 4 Mbps maximum
		InitialBitrate:    300000,  // 300 kbps initial
		BitrateChangeStep: 50000,   // 50 kbps step
	}
}

// NewController creates a new congestion controller
func NewController(config Config) *Controller {
	return &Controller{
		currentBitrate: config.InitialBitrate,
		targetBitrate:  config.InitialBitrate,
		minBitrate:     config.MinBitrate,
		maxBitrate:     config.MaxBitrate,
		state:          StateStable,
		rttEstimator:   NewRTTEstimator(),
		lossEstimator:  NewLossEstimator(),
		gcc:            NewGCC(),
	}
}

// OnTargetBitrateChange sets the callback for target bitrate changes
func (cc *Controller) OnTargetBitrateChange(callback func(uint32)) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.onTargetBitrateChange = callback
}

// UpdateRTT updates the RTT measurement
func (cc *Controller) UpdateRTT(rtt time.Duration) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.rttEstimator.Update(rtt)

	// Update GCC with RTT
	cc.gcc.UpdateRTT(rtt)
}

// UpdateLossRate updates the packet loss rate
func (cc *Controller) UpdateLossRate(lossRate float64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.lossEstimator.Update(lossRate)

	// Update GCC with loss rate
	cc.gcc.UpdateLossRate(lossRate)

	// Adjust state based on loss rate
	cc.adjustStateBasedOnLoss(lossRate)
}

// UpdateThroughput updates the measured throughput
func (cc *Controller) UpdateThroughput(throughput uint32) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Update GCC with throughput measurement
	cc.gcc.UpdateThroughput(throughput)
}

// GetTargetBitrate returns the current target bitrate
func (cc *Controller) GetTargetBitrate() uint32 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	// Return the minimum of current and GCC estimate
	gccEstimate := cc.gcc.GetEstimate()
	if gccEstimate > 0 && gccEstimate < cc.targetBitrate {
		return gccEstimate
	}

	return cc.targetBitrate
}

// GetCurrentBitrate returns the current bitrate
func (cc *Controller) GetCurrentBitrate() uint32 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.currentBitrate
}

// SetMinBitrate sets the minimum bitrate
func (cc *Controller) SetMinBitrate(bitrate uint32) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.minBitrate = bitrate
}

// SetMaxBitrate sets the maximum bitrate
func (cc *Controller) SetMaxBitrate(bitrate uint32) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.maxBitrate = bitrate
}

// adjustStateBasedOnLoss adjusts the congestion state based on loss rate
func (cc *Controller) adjustStateBasedOnLoss(lossRate float64) {
	switch {
	case lossRate > 0.1: // > 10% loss
		// Severe congestion - decrease quickly
		cc.state = StateDecrease
		cc.decreaseBitrate(0.5) // Decrease by 50%

	case lossRate > 0.05: // > 5% loss
		// Moderate congestion - decrease slowly
		cc.state = StateDecrease
		cc.decreaseBitrate(0.8) // Decrease by 20%

	case lossRate > 0.02: // > 2% loss
		// Mild congestion - hold bitrate
		cc.state = StateHold

	default:
		// Low loss - can increase if we were holding
		if cc.state == StateHold {
			cc.state = StateIncrease
		} else if cc.state == StateStable {
			// Gradually increase
			cc.increaseBitrate()
		}
	}
}

// decreaseBitrate decreases the target bitrate
func (cc *Controller) decreaseBitrate(factor float64) {
	newBitrate := uint32(float64(cc.targetBitrate) * factor)

	// Ensure minimum
	if newBitrate < cc.minBitrate {
		newBitrate = cc.minBitrate
	}

	if newBitrate != cc.targetBitrate {
		cc.targetBitrate = newBitrate
		cc.notifyBitrateChange()
		log.Printf("CongestionController: Decreased bitrate to %d bps (factor: %.2f)", newBitrate, factor)
	}
}

// increaseBitrate increases the target bitrate
func (cc *Controller) increaseBitrate() {
	// Only increase if GCC allows
	gccEstimate := cc.gcc.GetEstimate()
	if gccEstimate > cc.targetBitrate {
		// Use GCC estimate if higher
		cc.targetBitrate = gccEstimate
		if cc.targetBitrate > cc.maxBitrate {
			cc.targetBitrate = cc.maxBitrate
		}
		cc.notifyBitrateChange()
		log.Printf("CongestionController: Increased bitrate to %d bps (GCC estimate)", cc.targetBitrate)
	} else {
		// Gradual increase
		newBitrate := cc.targetBitrate + cc.gcc.GetBitrateIncrease()
		if newBitrate > cc.maxBitrate {
			newBitrate = cc.maxBitrate
		}
		if newBitrate > cc.targetBitrate {
			cc.targetBitrate = newBitrate
			cc.notifyBitrateChange()
			log.Printf("CongestionController: Increased bitrate to %d bps", newBitrate)
		}
	}
}

// notifyBitrateChange notifies the callback of bitrate change
func (cc *Controller) notifyBitrateChange() {
	if cc.onTargetBitrateChange != nil {
		cc.onTargetBitrateChange(cc.targetBitrate)
	}
}

// GetStats returns congestion control statistics
func (cc *Controller) GetStats() Stats {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return Stats{
		CurrentBitrate: cc.currentBitrate,
		TargetBitrate:  cc.targetBitrate,
		State:          cc.state.String(),
		RTT:            cc.rttEstimator.GetEstimate(),
		LossRate:       cc.lossEstimator.GetEstimate(),
		GCCEstimate:    cc.gcc.GetEstimate(),
	}
}

// Stats contains congestion control statistics
type Stats struct {
	CurrentBitrate uint32
	TargetBitrate  uint32
	State          string
	RTT            time.Duration
	LossRate       float64
	GCCEstimate    uint32
}

// Close closes the congestion controller
func (cc *Controller) Close() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.closed = true
	cc.rttEstimator.Close()
	cc.lossEstimator.Close()
	cc.gcc.Close()
}
