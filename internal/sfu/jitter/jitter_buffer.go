<<<<<<< HEAD
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
=======
package jitter

import (
	"sync"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// JitterBuffer manages ring buffers for RTP packets per track SSRC
type JitterBuffer struct {
	mu      sync.RWMutex
	buffers map[uint32]*Buffer
}

// NewJitterBuffer creates a new jitter buffer manager
func NewJitterBuffer() *JitterBuffer {
	return &JitterBuffer{
		buffers: make(map[uint32]*Buffer),
	}
}

// PushRTP pushes an RTP packet to the appropriate buffer and returns a NACK
// packet if the buffer determines that there are missing packets.
func (j *JitterBuffer) PushRTP(p *rtp.Packet) rtcp.Packet {
	j.mu.Lock()
	defer j.mu.Unlock()

	ssrc := p.SSRC
	buffer, ok := j.buffers[ssrc]
	if !ok {
		buffer = NewBuffer()
		j.buffers[ssrc] = buffer
	}

	return buffer.Push(p)
}

// GetPacket retrieves a packet from the RTP buffer by SSRC and sequence number
func (j *JitterBuffer) GetPacket(ssrc uint32, sn uint16) *rtp.Packet {
	j.mu.RLock()
	defer j.mu.RUnlock()

	buffer, ok := j.buffers[ssrc]
	if !ok {
		return nil
	}

	return buffer.GetPacket(sn)
}

// GetPackets retrieves multiple packets from the RTP buffer
func (j *JitterBuffer) GetPackets(ssrc uint32, sns []uint16) []*rtp.Packet {
	j.mu.RLock()
	defer j.mu.RUnlock()

	buffer, ok := j.buffers[ssrc]
	if !ok {
		return nil
	}

	return buffer.GetPackets(sns)
}

// RemoveBuffer removes a buffer for a specific SSRC
func (j *JitterBuffer) RemoveBuffer(ssrc uint32) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if buffer, ok := j.buffers[ssrc]; ok {
		buffer.Clear()
		delete(j.buffers, ssrc)
	}
}

// GetOrCreateBuffer gets an existing buffer or creates a new one for the SSRC
func (j *JitterBuffer) GetOrCreateBuffer(ssrc uint32) *Buffer {
	j.mu.Lock()
	defer j.mu.Unlock()

	buffer, ok := j.buffers[ssrc]
	if !ok {
		buffer = NewBuffer()
		j.buffers[ssrc] = buffer
	}

	return buffer
}

// HasBuffer checks if a buffer exists for the given SSRC
func (j *JitterBuffer) HasBuffer(ssrc uint32) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	_, ok := j.buffers[ssrc]
	return ok
}

// GetSSRCs returns all SSRCs currently being tracked
func (j *JitterBuffer) GetSSRCs() []uint32 {
	j.mu.RLock()
	defer j.mu.RUnlock()

	ssrcs := make([]uint32, 0, len(j.buffers))
	for ssrc := range j.buffers {
		ssrcs = append(ssrcs, ssrc)
	}

	return ssrcs
}

// Stats returns statistics for all buffers
func (j *JitterBuffer) Stats() map[uint32]struct {
	Received uint64
	Lost     uint64
} {
	j.mu.RLock()
	defer j.mu.RUnlock()

	stats := make(map[uint32]struct {
		Received uint64
		Lost     uint64
	})

	for ssrc, buffer := range j.buffers {
		received, lost := buffer.Stats()
		stats[ssrc] = struct {
			Received uint64
			Lost     uint64
		}{
			Received: received,
			Lost:     lost,
		}
	}

	return stats
}

// Clear removes all buffers
func (j *JitterBuffer) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()

	for _, buffer := range j.buffers {
		buffer.Clear()
	}

	j.buffers = make(map[uint32]*Buffer)
>>>>>>> main
}
