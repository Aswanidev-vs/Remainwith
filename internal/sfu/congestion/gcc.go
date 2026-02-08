// Package congestion provides Google Congestion Control (GCC) algorithm
package congestion

import (
	"log"
	"sync"
	"time"
)

// GCC implements the Google Congestion Control algorithm
// This is a simplified version of the algorithm described in:
// "A Google Congestion Control Algorithm for Real-Time Communication"
type GCC struct {
	mu sync.RWMutex

	// Current bitrate estimate (bps)
	estimate uint32

	// Minimum and maximum bitrate constraints
	minBitrate uint32
	maxBitrate uint32

	// Delay-based controller
	delayController *DelayController

	// Loss-based controller
	lossController *LossController

	// Rate controller
	rateController *RateController

	// Current state
	state GCCState

	// Last update time
	lastUpdate time.Time

	// Configuration
	config GCCConfig
}

// GCCState represents the GCC state
type GCCState int

const (
	// GCCStateIncrease indicates increasing bitrate
	GCCStateIncrease GCCState = iota
	// GCCStateDecrease indicates decreasing bitrate
	GCCStateDecrease
	// GCCStateHold indicates holding bitrate
	GCCStateHold
)

// GCCConfig contains GCC configuration
type GCCConfig struct {
	MinBitrate         uint32
	MaxBitrate         uint32
	InitialBitrate     uint32
	RateIncreaseStep   uint32
	RateDecreaseFactor float64
}

// DefaultGCCConfig returns default GCC configuration
func DefaultGCCConfig() GCCConfig {
	return GCCConfig{
		MinBitrate:         50000,   // 50 kbps
		MaxBitrate:         4000000, // 4 Mbps
		InitialBitrate:     300000,  // 300 kbps
		RateIncreaseStep:   50000,   // 50 kbps
		RateDecreaseFactor: 0.85,    // Decrease by 15%
	}
}

// NewGCC creates a new GCC instance
func NewGCC() *GCC {
	config := DefaultGCCConfig()
	return &GCC{
		estimate:        config.InitialBitrate,
		minBitrate:      config.MinBitrate,
		maxBitrate:      config.MaxBitrate,
		delayController: NewDelayController(),
		lossController:  NewLossController(),
		rateController:  NewRateController(config),
		state:           GCCStateIncrease,
		lastUpdate:      time.Now(),
		config:          config,
	}
}

// UpdateRTT updates GCC with a new RTT measurement
func (gcc *GCC) UpdateRTT(rtt time.Duration) {
	gcc.mu.Lock()
	defer gcc.mu.Unlock()

	gcc.delayController.UpdateRTT(rtt)
}

// UpdateLossRate updates GCC with a new loss rate
func (gcc *GCC) UpdateLossRate(lossRate float64) {
	gcc.mu.Lock()
	defer gcc.mu.Unlock()

	gcc.lossController.UpdateLossRate(lossRate)
}

// UpdateThroughput updates GCC with a throughput measurement
func (gcc *GCC) UpdateThroughput(throughput uint32) {
	gcc.mu.Lock()
	defer gcc.mu.Unlock()

	// Use throughput as upper bound
	if throughput > 0 && throughput < gcc.estimate {
		gcc.estimate = throughput
	}
}

// GetEstimate returns the current bitrate estimate
func (gcc *GCC) GetEstimate() uint32 {
	gcc.mu.RLock()
	defer gcc.mu.RUnlock()

	// Return the minimum of delay-based and loss-based estimates
	delayEstimate := gcc.delayController.GetEstimate()
	lossEstimate := gcc.lossController.GetEstimate()

	// Use the more conservative estimate
	estimate := gcc.estimate
	if delayEstimate > 0 && delayEstimate < estimate {
		estimate = delayEstimate
	}
	if lossEstimate > 0 && lossEstimate < estimate {
		estimate = lossEstimate
	}

	// Apply constraints
	if estimate < gcc.minBitrate {
		estimate = gcc.minBitrate
	}
	if estimate > gcc.maxBitrate {
		estimate = gcc.maxBitrate
	}

	return estimate
}

// GetBitrateIncrease returns the recommended bitrate increase
func (gcc *GCC) GetBitrateIncrease() uint32 {
	gcc.mu.RLock()
	defer gcc.mu.RUnlock()

	// Only increase if in increase state
	if gcc.state != GCCStateIncrease {
		return 0
	}

	return gcc.config.RateIncreaseStep
}

// Update runs the GCC algorithm
func (gcc *GCC) Update() {
	gcc.mu.Lock()
	defer gcc.mu.Unlock()

	now := time.Now()
	if now.Sub(gcc.lastUpdate) < 100*time.Millisecond {
		// Don't update too frequently
		return
	}
	gcc.lastUpdate = now

	// Get signals from controllers
	delaySignal := gcc.delayController.GetSignal()
	lossSignal := gcc.lossController.GetSignal()

	// Determine state based on signals
	switch {
	case lossSignal == SignalOveruse || delaySignal == SignalOveruse:
		gcc.state = GCCStateDecrease
		gcc.decreaseBitrate()

	case lossSignal == SignalUnderuse && delaySignal == SignalUnderuse:
		gcc.state = GCCStateIncrease
		gcc.increaseBitrate()

	default:
		gcc.state = GCCStateHold
	}

	// Update rate controller
	gcc.rateController.Update(gcc.state, gcc.estimate)
}

// decreaseBitrate decreases the bitrate estimate
func (gcc *GCC) decreaseBitrate() {
	newEstimate := uint32(float64(gcc.estimate) * gcc.config.RateDecreaseFactor)
	if newEstimate < gcc.minBitrate {
		newEstimate = gcc.minBitrate
	}

	if newEstimate != gcc.estimate {
		log.Printf("GCC: Decreasing bitrate from %d to %d bps", gcc.estimate, newEstimate)
		gcc.estimate = newEstimate
	}
}

// increaseBitrate increases the bitrate estimate
func (gcc *GCC) increaseBitrate() {
	newEstimate := gcc.estimate + gcc.config.RateIncreaseStep
	if newEstimate > gcc.maxBitrate {
		newEstimate = gcc.maxBitrate
	}

	if newEstimate != gcc.estimate {
		log.Printf("GCC: Increasing bitrate from %d to %d bps", gcc.estimate, newEstimate)
		gcc.estimate = newEstimate
	}
}

// Close closes the GCC instance
func (gcc *GCC) Close() {
	// Nothing to clean up
}

// DelayController implements the delay-based controller
type DelayController struct {
	mu sync.RWMutex

	// RTT measurements
	rttHistory []time.Duration
	maxHistory int

	// Current estimate
	estimate uint32

	// Threshold for overuse detection
	overuseThreshold time.Duration

	// Last update
	lastRTT time.Duration
}

// NewDelayController creates a new delay controller
func NewDelayController() *DelayController {
	return &DelayController{
		rttHistory:       make([]time.Duration, 0, 100),
		maxHistory:       100,
		overuseThreshold: 50 * time.Millisecond, // 50ms threshold
	}
}

// UpdateRTT updates with a new RTT measurement
func (dc *DelayController) UpdateRTT(rtt time.Duration) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	dc.lastRTT = rtt

	// Add to history
	dc.rttHistory = append(dc.rttHistory, rtt)
	if len(dc.rttHistory) > dc.maxHistory {
		dc.rttHistory = dc.rttHistory[1:]
	}

	// Calculate trend
	if len(dc.rttHistory) >= 10 {
		// Simple linear regression to detect trend
		trend := dc.calculateTrend()
		if trend > dc.overuseThreshold {
			// Overuse detected
		}
	}
}

// GetSignal returns the current congestion signal
func (dc *DelayController) GetSignal() Signal {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	if len(dc.rttHistory) < 10 {
		return SignalNormal
	}

	trend := dc.calculateTrend()
	if trend > dc.overuseThreshold {
		return SignalOveruse
	}
	if trend < -dc.overuseThreshold {
		return SignalUnderuse
	}

	return SignalNormal
}

// GetEstimate returns the delay-based bitrate estimate
func (dc *DelayController) GetEstimate() uint32 {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.estimate
}

// calculateTrend calculates the RTT trend using simple linear regression
func (dc *DelayController) calculateTrend() time.Duration {
	if len(dc.rttHistory) < 2 {
		return 0
	}

	// Use last 10 samples
	n := min(10, len(dc.rttHistory))
	samples := dc.rttHistory[len(dc.rttHistory)-n:]

	// Simple trend: difference between average of first half and second half
	half := n / 2
	if half == 0 {
		return 0
	}

	var firstHalf, secondHalf time.Duration
	for i := 0; i < half; i++ {
		firstHalf += samples[i]
		secondHalf += samples[i+half]
	}

	avgFirst := firstHalf / time.Duration(half)
	avgSecond := secondHalf / time.Duration(half)

	return avgSecond - avgFirst
}

// LossController implements the loss-based controller
type LossController struct {
	mu sync.RWMutex

	// Current loss rate
	lossRate float64

	// Thresholds
	severeThreshold   float64
	moderateThreshold float64
	mildThreshold     float64

	// Estimate
	estimate uint32
}

// NewLossController creates a new loss controller
func NewLossController() *LossController {
	return &LossController{
		lossRate:          0,
		severeThreshold:   0.10, // 10%
		moderateThreshold: 0.05, // 5%
		mildThreshold:     0.02, // 2%
	}
}

// UpdateLossRate updates with a new loss rate
func (lc *LossController) UpdateLossRate(lossRate float64) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.lossRate = lossRate

	// Adjust estimate based on loss rate
	switch {
	case lossRate > lc.severeThreshold:
		// Severe loss - reduce significantly
		lc.estimate = uint32(float64(lc.estimate) * 0.5)

	case lossRate > lc.moderateThreshold:
		// Moderate loss - reduce moderately
		lc.estimate = uint32(float64(lc.estimate) * 0.8)

	case lossRate > lc.mildThreshold:
		// Mild loss - hold current
		// Don't change estimate

	default:
		// Low loss - can increase (handled by rate controller)
	}
}

// GetSignal returns the current congestion signal
func (lc *LossController) GetSignal() Signal {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	switch {
	case lc.lossRate > lc.moderateThreshold:
		return SignalOveruse
	case lc.lossRate < lc.mildThreshold:
		return SignalUnderuse
	default:
		return SignalNormal
	}
}

// GetEstimate returns the loss-based bitrate estimate
func (lc *LossController) GetEstimate() uint32 {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.estimate
}

// RateController manages the bitrate changes
type RateController struct {
	mu sync.RWMutex

	config GCCConfig
}

// NewRateController creates a new rate controller
func NewRateController(config GCCConfig) *RateController {
	return &RateController{
		config: config,
	}
}

// Update updates the rate based on state
func (rc *RateController) Update(state GCCState, currentEstimate uint32) uint32 {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	switch state {
	case GCCStateIncrease:
		return currentEstimate + rc.config.RateIncreaseStep
	case GCCStateDecrease:
		return uint32(float64(currentEstimate) * rc.config.RateDecreaseFactor)
	default:
		return currentEstimate
	}
}

// Signal represents the congestion signal
type Signal int

const (
	// SignalNormal indicates normal operation
	SignalNormal Signal = iota
	// SignalOveruse indicates congestion
	SignalOveruse
	// SignalUnderuse indicates underutilization
	SignalUnderuse
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
