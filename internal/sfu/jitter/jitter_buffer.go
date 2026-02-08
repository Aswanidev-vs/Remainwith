// Package jitter provides adaptive jitter buffer for RTP packets
package jitter

import (
	"container/heap"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// Packet represents an RTP packet with arrival time
type Packet struct {
	*rtp.Packet
	ArrivalTime    time.Time
	SequenceNumber uint16
}

// PacketHeap implements a min-heap for packet ordering
type PacketHeap []*Packet

func (h PacketHeap) Len() int           { return len(h) }
func (h PacketHeap) Less(i, j int) bool { return h[i].SequenceNumber < h[j].SequenceNumber }
func (h PacketHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *PacketHeap) Push(x interface{}) {
	*h = append(*h, x.(*Packet))
}

func (h *PacketHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// JitterBuffer provides adaptive buffering for RTP packets
type JitterBuffer struct {
	mu sync.RWMutex

	// Packet storage
	packets PacketHeap
	maxSize int

	// Timing
	minDelay     time.Duration
	maxDelay     time.Duration
	currentDelay time.Duration
	lastReadTime time.Time

	// Statistics
	stats Stats

	// Control
	closed bool
	done   chan struct{}
}

// Stats contains jitter buffer statistics
type Stats struct {
	PacketsReceived uint32
	PacketsDropped  uint32
	PacketsLate     uint32
	JitterEstimate  uint32 // in milliseconds
	BufferSize      int
}

// Config contains jitter buffer configuration
type Config struct {
	MinDelay   time.Duration
	MaxDelay   time.Duration
	MaxSize    int
	SampleRate uint32
}

// DefaultConfig returns default jitter buffer configuration
func DefaultConfig() Config {
	return Config{
		MinDelay:   20 * time.Millisecond,  // 20ms minimum
		MaxDelay:   500 * time.Millisecond, // 500ms maximum
		MaxSize:    1000,                   // Max 1000 packets
		SampleRate: 90000,                  // Default 90kHz for video
	}
}

// New creates a new jitter buffer
func New(config Config) *JitterBuffer {
	jb := &JitterBuffer{
		packets:      make(PacketHeap, 0),
		maxSize:      config.MaxSize,
		minDelay:     config.MinDelay,
		maxDelay:     config.MaxDelay,
		currentDelay: config.MinDelay,
		lastReadTime: time.Now(),
		done:         make(chan struct{}),
	}
	heap.Init(&jb.packets)
	return jb
}

// Push adds a packet to the jitter buffer
func (jb *JitterBuffer) Push(packet *rtp.Packet, arrivalTime time.Time) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if jb.closed {
		return false
	}

	// Check if buffer is full
	if len(jb.packets) >= jb.maxSize {
		jb.stats.PacketsDropped++
		// Remove oldest packet to make room
		heap.Pop(&jb.packets)
	}

	// Create packet wrapper
	p := &Packet{
		Packet:         packet,
		ArrivalTime:    arrivalTime,
		SequenceNumber: packet.SequenceNumber,
	}

	// Add to heap
	heap.Push(&jb.packets, p)
	jb.stats.PacketsReceived++

	// Update jitter estimate
	jb.updateJitterEstimate(packet.Timestamp, arrivalTime)

	return true
}

// Pop removes and returns the next packet if it's time to play it
func (jb *JitterBuffer) Pop(now time.Time) *rtp.Packet {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if jb.closed || len(jb.packets) == 0 {
		return nil
	}

	// Peek at the next packet
	next := jb.packets[0]

	// Check if it's time to play this packet
	playoutTime := jb.calculatePlayoutTime(next)

	if now.Before(playoutTime) {
		// Not time yet
		return nil
	}

	// Remove and return the packet
	heap.Pop(&jb.packets)
	jb.lastReadTime = now
	jb.stats.BufferSize = len(jb.packets)

	return next.Packet
}

// calculatePlayoutTime calculates when a packet should be played
func (jb *JitterBuffer) calculatePlayoutTime(p *Packet) time.Time {
	// Simple playout time calculation based on current delay
	return p.ArrivalTime.Add(jb.currentDelay)
}

// updateJitterEstimate updates the jitter estimate based on packet arrival
func (jb *JitterBuffer) updateJitterEstimate(timestamp uint32, arrivalTime time.Time) {
	// Simplified jitter calculation
	// In production, use more sophisticated algorithms like in RFC 3550
	if jb.lastReadTime.IsZero() {
		return
	}

	// Calculate inter-arrival jitter
	// This is a simplified version - production should use proper RFC 3550 algorithm
}

// GetStats returns current statistics
func (jb *JitterBuffer) GetStats() Stats {
	jb.mu.RLock()
	defer jb.mu.RUnlock()

	return Stats{
		PacketsReceived: jb.stats.PacketsReceived,
		PacketsDropped:  jb.stats.PacketsDropped,
		PacketsLate:     jb.stats.PacketsLate,
		JitterEstimate:  jb.stats.JitterEstimate,
		BufferSize:      len(jb.packets),
	}
}

// Size returns the current number of packets in buffer
func (jb *JitterBuffer) Size() int {
	jb.mu.RLock()
	defer jb.mu.RUnlock()
	return len(jb.packets)
}

// Close closes the jitter buffer
func (jb *JitterBuffer) Close() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.closed = true
	close(jb.done)
}

// Done returns a channel that's closed when the buffer is closed
func (jb *JitterBuffer) Done() <-chan struct{} {
	return jb.done
}

// SetDelay adjusts the target delay dynamically
func (jb *JitterBuffer) SetDelay(delay time.Duration) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if delay < jb.minDelay {
		delay = jb.minDelay
	}
	if delay > jb.maxDelay {
		delay = jb.maxDelay
	}
	jb.currentDelay = delay
}

// GetCurrentDelay returns the current target delay
func (jb *JitterBuffer) GetCurrentDelay() time.Duration {
	jb.mu.RLock()
	defer jb.mu.RUnlock()
	return jb.currentDelay
}
