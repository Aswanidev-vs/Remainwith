<<<<<<< HEAD
// Package congestion provides congestion control for WebRTC SFU
=======
>>>>>>> main
package congestion

import (
	"log"
	"sync"
	"time"
<<<<<<< HEAD
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
=======

	"Remainwith/internal/sfu/simulcast"
)

// Controller manages congestion control for the SFU
type Controller struct {
	mu sync.RWMutex

	// Simulcast manager for layer selection
	simulcastManager *simulcast.Manager

	// Subscriber states indexed by subscriberID
	subscriberStates map[string]*SubscriberState

	// Configuration
	config Config

	// Control loop ticker
	ticker *time.Ticker
	done   chan struct{}
}

// Config contains congestion controller configuration
type Config struct {
	// InitialBitrate is the initial bitrate estimate (bps)
	InitialBitrate uint32
	// MinBitrate is the minimum allowed bitrate (bps)
	MinBitrate uint32
	// MaxBitrate is the maximum allowed bitrate (bps)
	MaxBitrate uint32
	// ProbeInterval is the interval for bandwidth probing
	ProbeInterval time.Duration
	// UpdateInterval is the interval for congestion control updates
	UpdateInterval time.Duration
}

// DefaultConfig returns default congestion control configuration
func DefaultConfig() Config {
	return Config{
		InitialBitrate: 2000000,  // 2 Mbps
		MinBitrate:     150000,   // 150 kbps
		MaxBitrate:     10000000, // 10 Mbps
		ProbeInterval:  5 * time.Second,
		UpdateInterval: 1 * time.Second,
	}
}

// SubscriberState tracks congestion state for a subscriber
type SubscriberState struct {
	SubscriberID string

	// Current estimated bandwidth (bps)
	EstimatedBandwidth uint32

	// Target bitrate for each track
	TrackTargets map[string]uint32

	// Loss rate (0.0 - 1.0)
	LossRate float64

	// RTT in milliseconds
	RTT uint32

	// Last update time
	LastUpdate time.Time

	// Congestion state
	State CongestionState
}

// CongestionState represents the congestion state
type CongestionState int

const (
	// StateNormal indicates normal operation
	StateNormal CongestionState = iota
	// StateWarning indicates approaching congestion
	StateWarning
	// StateCongested indicates congestion detected
	StateCongested
	// StateRecovering indicates recovering from congestion
	StateRecovering
)

func (s CongestionState) String() string {
	switch s {
	case StateNormal:
		return "normal"
	case StateWarning:
		return "warning"
	case StateCongested:
		return "congested"
	case StateRecovering:
		return "recovering"
>>>>>>> main
	default:
		return "unknown"
	}
}

<<<<<<< HEAD
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
=======
// NewController creates a new congestion controller
func NewController(simulcastManager *simulcast.Manager, config Config) *Controller {
	if config.InitialBitrate == 0 {
		config = DefaultConfig()
	}

	c := &Controller{
		simulcastManager: simulcastManager,
		subscriberStates: make(map[string]*SubscriberState),
		config:           config,
		ticker:           time.NewTicker(config.UpdateInterval),
		done:             make(chan struct{}),
	}

	go c.controlLoop()

	return c
}

// controlLoop runs the congestion control algorithm
func (c *Controller) controlLoop() {
	for {
		select {
		case <-c.ticker.C:
			c.updateAllSubscribers()
		case <-c.done:
			return
		}
	}
}

// updateAllSubscribers updates congestion control for all subscribers
func (c *Controller) updateAllSubscribers() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for subscriberID, state := range c.subscriberStates {
		c.updateSubscriber(subscriberID, state)
	}
}

// updateSubscriber updates congestion control for a single subscriber
func (c *Controller) updateSubscriber(subscriberID string, state *SubscriberState) {
	// Calculate new target bitrate based on congestion state
	newTarget := c.calculateTargetBitrate(state)

	// Update simulcast manager with new bandwidth limit
	c.simulcastManager.SetSubscriberBandwidth(subscriberID, newTarget)

	// Update track targets
	state.EstimatedBandwidth = newTarget
	state.LastUpdate = time.Now()

	log.Printf("CongestionController: Subscriber %s - state: %s, bandwidth: %d bps, loss: %.2f%%",
		subscriberID, state.State.String(), newTarget, state.LossRate*100)
}

// calculateTargetBitrate calculates the target bitrate based on congestion state
func (c *Controller) calculateTargetBitrate(state *SubscriberState) uint32 {
	current := state.EstimatedBandwidth
	if current == 0 {
		current = c.config.InitialBitrate
	}

	switch state.State {
	case StateNormal:
		// Gradually increase bitrate
		newBitrate := uint32(float64(current) * 1.05)
		if newBitrate > c.config.MaxBitrate {
			newBitrate = c.config.MaxBitrate
		}
		return newBitrate

	case StateWarning:
		// Hold steady
		return current

	case StateCongested:
		// Reduce bitrate significantly
		newBitrate := uint32(float64(current) * 0.7)
		if newBitrate < c.config.MinBitrate {
			newBitrate = c.config.MinBitrate
		}
		return newBitrate

	case StateRecovering:
		// Gradually increase
		newBitrate := uint32(float64(current) * 1.02)
		if newBitrate > c.config.MaxBitrate {
			newBitrate = c.config.MaxBitrate
		}
		return newBitrate

	default:
		return current
	}
}

// AddSubscriber adds a subscriber to congestion control
func (c *Controller) AddSubscriber(subscriberID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.subscriberStates[subscriberID] = &SubscriberState{
		SubscriberID:       subscriberID,
		EstimatedBandwidth: c.config.InitialBitrate,
		TrackTargets:       make(map[string]uint32),
		State:              StateNormal,
		LastUpdate:         time.Now(),
	}

	// Set initial bandwidth in simulcast manager
	c.simulcastManager.SetSubscriberBandwidth(subscriberID, c.config.InitialBitrate)

	log.Printf("CongestionController: Added subscriber %s", subscriberID)
}

// RemoveSubscriber removes a subscriber from congestion control
func (c *Controller) RemoveSubscriber(subscriberID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.subscriberStates, subscriberID)
	log.Printf("CongestionController: Removed subscriber %s", subscriberID)
}

// UpdateRTT updates the RTT measurement for a subscriber
func (c *Controller) UpdateRTT(subscriberID string, rtt uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if state, ok := c.subscriberStates[subscriberID]; ok {
		state.RTT = rtt

		// Detect congestion based on RTT
		if rtt > 300 {
			// High RTT indicates congestion
			if state.State != StateCongested {
				state.State = StateCongested
				log.Printf("CongestionController: Subscriber %s entering congested state (RTT: %dms)", subscriberID, rtt)
			}
		} else if rtt > 150 {
			// Elevated RTT
			if state.State == StateNormal {
				state.State = StateWarning
				log.Printf("CongestionController: Subscriber %s entering warning state (RTT: %dms)", subscriberID, rtt)
			}
		} else {
			// Normal RTT - recover if we were congested
			if state.State == StateCongested || state.State == StateWarning {
				state.State = StateRecovering
				log.Printf("CongestionController: Subscriber %s recovering (RTT: %dms)", subscriberID, rtt)
			} else if state.State == StateRecovering {
				// After some time in recovering, go back to normal
				if time.Since(state.LastUpdate) > 5*time.Second {
					state.State = StateNormal
					log.Printf("CongestionController: Subscriber %s back to normal (RTT: %dms)", subscriberID, rtt)
				}
			}
>>>>>>> main
		}
	}
}

<<<<<<< HEAD
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
=======
// UpdateLossRate updates the packet loss rate for a subscriber
func (c *Controller) UpdateLossRate(subscriberID string, lossRate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if state, ok := c.subscriberStates[subscriberID]; ok {
		state.LossRate = lossRate

		// Detect congestion based on loss rate
		if lossRate > 0.05 {
			// > 5% loss indicates congestion
			if state.State != StateCongested {
				state.State = StateCongested
				log.Printf("CongestionController: Subscriber %s entering congested state (loss: %.2f%%)", subscriberID, lossRate*100)
			}
		} else if lossRate > 0.02 {
			// > 2% loss is warning
			if state.State == StateNormal {
				state.State = StateWarning
				log.Printf("CongestionController: Subscriber %s entering warning state (loss: %.2f%%)", subscriberID, lossRate*100)
			}
>>>>>>> main
		}
	}
}

<<<<<<< HEAD
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
=======
// UpdateBitrateEstimate updates the bitrate estimate from REMB or transport-cc
func (c *Controller) UpdateBitrateEstimate(subscriberID string, bitrate uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if state, ok := c.subscriberStates[subscriberID]; ok {
		// Use the minimum of estimated and measured bitrate
		if bitrate < state.EstimatedBandwidth {
			state.EstimatedBandwidth = bitrate
			state.State = StateWarning
			log.Printf("CongestionController: Subscriber %s bitrate reduced to %d bps (external estimate)", subscriberID, bitrate)
		}
	}
}

// GetSubscriberState returns the congestion state for a subscriber
func (c *Controller) GetSubscriberState(subscriberID string) (*SubscriberState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.subscriberStates[subscriberID]
	return state, ok
}

// GetAllSubscriberStates returns all subscriber states
func (c *Controller) GetAllSubscriberStates() map[string]*SubscriberState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	states := make(map[string]*SubscriberState)
	for k, v := range c.subscriberStates {
		states[k] = v
	}

	return states
}

// Close closes the congestion controller
func (c *Controller) Close() {
	close(c.done)
	c.ticker.Stop()
>>>>>>> main
}
