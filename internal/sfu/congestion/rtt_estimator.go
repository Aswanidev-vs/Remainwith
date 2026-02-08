// Package congestion provides RTT estimation for congestion control
package congestion

import (
	"sync"
	"time"
)

// RTTEstimator estimates round-trip time using exponential moving average
type RTTEstimator struct {
	mu sync.RWMutex

	// Current RTT estimate
	currentRTT time.Duration

	// Smoothed RTT (SRTT)
	smoothedRTT time.Duration

	// RTT variance
	rttVar time.Duration

	// Minimum RTT seen
	minRTT time.Duration

	// Maximum RTT seen
	maxRTT time.Duration

	// Alpha factor for exponential moving average (typically 0.125)
	alpha float64

	// Beta factor for variance (typically 0.25)
	beta float64

	// Initial RTT
	initialRTT time.Duration

	// Sample count
	sampleCount int
}

// NewRTTEstimator creates a new RTT estimator
func NewRTTEstimator() *RTTEstimator {
	return &RTTEstimator{
		currentRTT:  0,
		smoothedRTT: 0,
		rttVar:      0,
		minRTT:      0,
		maxRTT:      0,
		alpha:       0.125,
		beta:        0.25,
		initialRTT:  100 * time.Millisecond,
	}
}

// Update updates the RTT estimate with a new sample
func (r *RTTEstimator) Update(rtt time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sampleCount++

	// First sample
	if r.sampleCount == 1 {
		r.smoothedRTT = rtt
		r.rttVar = rtt / 2
		r.minRTT = rtt
		r.maxRTT = rtt
		r.currentRTT = rtt
		return
	}

	// Update min/max
	if rtt < r.minRTT {
		r.minRTT = rtt
	}
	if rtt > r.maxRTT {
		r.maxRTT = rtt
	}

	// RFC 6298 algorithm for SRTT and RTTVAR
	// SRTT = (1 - alpha) * SRTT + alpha * R'
	// RTTVAR = (1 - beta) * RTTVAR + beta * |SRTT - R'|
	r.rttVar = time.Duration((1-r.beta)*float64(r.rttVar) + r.beta*float64(abs(r.smoothedRTT-rtt)))
	r.smoothedRTT = time.Duration((1-r.alpha)*float64(r.smoothedRTT) + r.alpha*float64(rtt))

	// Current RTT is the smoothed value
	r.currentRTT = r.smoothedRTT
}

// GetEstimate returns the current RTT estimate
func (r *RTTEstimator) GetEstimate() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.sampleCount == 0 {
		return r.initialRTT
	}

	return r.currentRTT
}

// GetSmoothedRTT returns the smoothed RTT
func (r *RTTEstimator) GetSmoothedRTT() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.smoothedRTT
}

// GetRTTVar returns the RTT variance
func (r *RTTEstimator) GetRTTVar() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rttVar
}

// GetMinRTT returns the minimum RTT seen
func (r *RTTEstimator) GetMinRTT() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.minRTT
}

// GetMaxRTT returns the maximum RTT seen
func (r *RTTEstimator) GetMaxRTT() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.maxRTT
}

// GetStats returns RTT statistics
func (r *RTTEstimator) GetStats() RTTStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RTTStats{
		CurrentRTT:  r.currentRTT,
		SmoothedRTT: r.smoothedRTT,
		RTTVar:      r.rttVar,
		MinRTT:      r.minRTT,
		MaxRTT:      r.maxRTT,
		SampleCount: r.sampleCount,
	}
}

// RTTStats contains RTT statistics
type RTTStats struct {
	CurrentRTT  time.Duration
	SmoothedRTT time.Duration
	RTTVar      time.Duration
	MinRTT      time.Duration
	MaxRTT      time.Duration
	SampleCount int
}

// Close closes the RTT estimator
func (r *RTTEstimator) Close() {
	// Nothing to clean up
}

// abs returns the absolute value of a duration
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
