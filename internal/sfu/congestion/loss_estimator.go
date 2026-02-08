// Package congestion provides packet loss estimation for congestion control
package congestion

import (
	"sync"
	"time"
)

// LossEstimator estimates packet loss rate
type LossEstimator struct {
	mu sync.RWMutex

	// Current loss rate (0.0 to 1.0)
	currentLoss float64

	// Smoothed loss rate
	smoothedLoss float64

	// Loss history for moving average
	lossHistory []float64
	historySize int

	// Total packets received
	totalPackets uint32

	// Total packets lost
	totalLost uint32

	// Window for recent loss calculation
	windowSize    time.Duration
	windowPackets uint32
	windowLost    uint32
	windowStart   time.Time

	// Alpha for exponential smoothing
	alpha float64
}

// NewLossEstimator creates a new loss estimator
func NewLossEstimator() *LossEstimator {
	return &LossEstimator{
		currentLoss:  0,
		smoothedLoss: 0,
		lossHistory:  make([]float64, 0, 100),
		historySize:  100,
		windowSize:   1 * time.Second,
		windowStart:  time.Now(),
		alpha:        0.3,
	}
}

// Update updates the loss estimate
// packetsReceived: number of packets received in this interval
// packetsExpected: number of packets expected (based on sequence numbers)
func (le *LossEstimator) UpdateWithCount(packetsReceived, packetsExpected uint32) {
	le.mu.Lock()
	defer le.mu.Unlock()

	if packetsExpected == 0 {
		return
	}

	// Calculate loss for this interval
	lost := packetsExpected - packetsReceived
	if lost < 0 {
		lost = 0 // Can happen due to out-of-order packets
	}

	le.totalPackets += packetsReceived
	le.totalLost += uint32(lost)

	// Calculate instantaneous loss rate
	instantLoss := float64(lost) / float64(packetsExpected)

	// Update smoothed loss using exponential moving average
	le.smoothedLoss = le.alpha*instantLoss + (1-le.alpha)*le.smoothedLoss

	// Clamp to valid range
	if le.smoothedLoss < 0 {
		le.smoothedLoss = 0
	}
	if le.smoothedLoss > 1 {
		le.smoothedLoss = 1
	}

	le.currentLoss = le.smoothedLoss

	// Add to history
	le.lossHistory = append(le.lossHistory, instantLoss)
	if len(le.lossHistory) > le.historySize {
		le.lossHistory = le.lossHistory[1:]
	}
}

// Update updates with a direct loss rate value
func (le *LossEstimator) Update(lossRate float64) {
	le.mu.Lock()
	defer le.mu.Unlock()

	// Clamp to valid range
	if lossRate < 0 {
		lossRate = 0
	}
	if lossRate > 1 {
		lossRate = 1
	}

	// Update smoothed loss
	le.smoothedLoss = le.alpha*lossRate + (1-le.alpha)*le.smoothedLoss
	le.currentLoss = le.smoothedLoss
}

// GetEstimate returns the current loss rate estimate
func (le *LossEstimator) GetEstimate() float64 {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLoss
}

// GetSmoothedLoss returns the smoothed loss rate
func (le *LossEstimator) GetSmoothedLoss() float64 {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.smoothedLoss
}

// GetTotalStats returns total packet statistics
func (le *LossEstimator) GetTotalStats() (totalPackets, totalLost uint32) {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.totalPackets, le.totalLost
}

// GetStats returns loss statistics
func (le *LossEstimator) GetStats() LossStats {
	le.mu.RLock()
	defer le.mu.RUnlock()

	// Calculate average from history
	var avgLoss float64
	if len(le.lossHistory) > 0 {
		sum := 0.0
		for _, loss := range le.lossHistory {
			sum += loss
		}
		avgLoss = sum / float64(len(le.lossHistory))
	}

	return LossStats{
		CurrentLoss:  le.currentLoss,
		SmoothedLoss: le.smoothedLoss,
		AverageLoss:  avgLoss,
		TotalPackets: le.totalPackets,
		TotalLost:    le.totalLost,
		LossRatio:    float64(le.totalLost) / float64(max(1, le.totalPackets)),
	}
}

// LossStats contains loss statistics
type LossStats struct {
	CurrentLoss  float64
	SmoothedLoss float64
	AverageLoss  float64
	TotalPackets uint32
	TotalLost    uint32
	LossRatio    float64
}

// IsSevere returns true if loss rate is severe (> 10%)
func (le *LossEstimator) IsSevere() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLoss > 0.1
}

// IsModerate returns true if loss rate is moderate (> 5%)
func (le *LossEstimator) IsModerate() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLoss > 0.05
}

// IsMild returns true if loss rate is mild (> 2%)
func (le *LossEstimator) IsMild() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLoss > 0.02
}

// Close closes the loss estimator
func (le *LossEstimator) Close() {
	// Nothing to clean up
}

// max returns the maximum of two uint32 values
func max(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

// CalculateLossRate calculates loss rate from sequence number gaps
func CalculateLossRate(receivedSeqs []uint16) float64 {
	if len(receivedSeqs) < 2 {
		return 0
	}

	// Sort sequence numbers
	sorted := make([]uint16, len(receivedSeqs))
	copy(sorted, receivedSeqs)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if seqLess(sorted[j], sorted[i]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate expected vs received
	minSeq := sorted[0]
	maxSeq := sorted[len(sorted)-1]

	// Handle wraparound
	var expected uint32
	if maxSeq >= minSeq {
		expected = uint32(maxSeq - minSeq + 1)
	} else {
		// Wraparound occurred
		expected = uint32(65535-minSeq+1) + uint32(maxSeq) + 1
	}

	received := uint32(len(sorted))

	if expected == 0 {
		return 0
	}

	lost := expected - received
	if lost > expected {
		lost = expected // Sanity check
	}

	return float64(lost) / float64(expected)
}

// seqLess compares two sequence numbers with wraparound handling
func seqLess(a, b uint16) bool {
	diff := int16(a - b)
	return diff < 0
}
