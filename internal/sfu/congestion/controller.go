package congestion

import (
	"log"
	"sync"
	"time"

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
	default:
		return "unknown"
	}
}

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
		}
	}
}

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
		}
	}
}

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
}
