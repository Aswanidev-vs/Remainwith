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
}
