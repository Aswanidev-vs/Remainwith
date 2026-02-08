package jitter

import (
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

const (
	// DefaultBufferSize is the default size of the jitter buffer
	DefaultBufferSize = 1024
	// MaxNackPackets is the maximum number of packets to request in a NACK
	MaxNackPackets = 17
	// NackThreshold is the number of missing packets before sending NACK
	NackThreshold = 2
	// MaxPacketAge is the maximum age of a packet in the buffer
	MaxPacketAge = 1000 // milliseconds
)

// Buffer is a ring buffer for RTP packets
type Buffer struct {
	mu sync.RWMutex

	// packets stores RTP packets indexed by sequence number
	packets map[uint16]*rtp.Packet

	// sequence number tracking
	lastSequenceNumber uint16
	initialized        bool

	// NACK tracking
	missingPackets map[uint16]time.Time
	nackCount      int

	// buffer configuration
	size int

	// metrics
	packetsReceived uint64
	packetsLost     uint64
}

// NewBuffer creates a new jitter buffer
func NewBuffer() *Buffer {
	return &Buffer{
		packets:        make(map[uint16]*rtp.Packet),
		missingPackets: make(map[uint16]time.Time),
		size:           DefaultBufferSize,
	}
}

// Push adds a packet to the buffer and returns a NACK packet if needed
func (b *Buffer) Push(p *rtp.Packet) rtcp.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.packetsReceived++

	// Initialize on first packet
	if !b.initialized {
		b.lastSequenceNumber = p.SequenceNumber
		b.initialized = true
		b.packets[p.SequenceNumber] = p
		return nil
	}

	// Calculate sequence number difference
	diff := int16(p.SequenceNumber - b.lastSequenceNumber)

	// Handle out-of-order packets
	if diff < 0 {
		// Late packet - add it if we have space
		if len(b.packets) < b.size {
			b.packets[p.SequenceNumber] = p
			// Remove from missing packets if it was there
			delete(b.missingPackets, p.SequenceNumber)
		}
		return nil
	}

	// Check for missing packets
	if diff > 1 {
		// We have missing packets
		for i := int16(1); i < diff; i++ {
			missingSN := b.lastSequenceNumber + uint16(i)
			if _, ok := b.packets[missingSN]; !ok {
				b.missingPackets[missingSN] = time.Now()
				b.packetsLost++
			}
		}
	}

	// Add current packet
	b.packets[p.SequenceNumber] = p
	b.lastSequenceNumber = p.SequenceNumber

	// Clean up old packets
	b.cleanup()

	// Check if we need to send NACK
	return b.generateNACK()
}

// GetPacket retrieves a packet by sequence number
func (b *Buffer) GetPacket(sn uint16) *rtp.Packet {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if p, ok := b.packets[sn]; ok {
		return p
	}
	return nil
}

// GetPackets retrieves multiple packets by sequence numbers
func (b *Buffer) GetPackets(sns []uint16) []*rtp.Packet {
	b.mu.RLock()
	defer b.mu.RUnlock()

	packets := make([]*rtp.Packet, 0, len(sns))
	for _, sn := range sns {
		if p, ok := b.packets[sn]; ok {
			packets = append(packets, p)
		}
	}
	return packets
}

// generateNACK creates a NACK packet for missing packets
func (b *Buffer) generateNACK() rtcp.Packet {
	if len(b.missingPackets) < NackThreshold {
		return nil
	}

	// Get missing sequence numbers that haven't been NACKed recently
	var nackSNs []uint16
	now := time.Now()

	for sn, lastNack := range b.missingPackets {
		// Only NACK if it's been at least 10ms since last NACK
		if now.Sub(lastNack) > 10*time.Millisecond {
			nackSNs = append(nackSNs, sn)
			b.missingPackets[sn] = now
		}

		// Limit number of packets in NACK
		if len(nackSNs) >= MaxNackPackets {
			break
		}
	}

	if len(nackSNs) == 0 {
		return nil
	}

	// Create NACK pairs
	nackPairs := createNackPairs(nackSNs)

	return &rtcp.TransportLayerNack{
		MediaSSRC:  0, // Will be set by caller
		SenderSSRC: 0, // Will be set by caller
		Nacks:      nackPairs,
	}
}

// createNackPairs creates NACK pairs from sequence numbers
func createNackPairs(sns []uint16) []rtcp.NackPair {
	if len(sns) == 0 {
		return nil
	}

	var pairs []rtcp.NackPair
	var currentPair rtcp.NackPair
	var lastSN uint16

	for i, sn := range sns {
		if i == 0 {
			currentPair.PacketID = sn
			lastSN = sn
			continue
		}

		// Check if this SN is within the bit mask range (16 bits)
		diff := sn - currentPair.PacketID
		if diff <= 16 && sn == lastSN+1 {
			// Set the bit in the lost packets mask
			currentPair.LostPackets |= 1 << (diff - 1)
			lastSN = sn
		} else {
			// Start a new pair
			pairs = append(pairs, currentPair)
			currentPair = rtcp.NackPair{
				PacketID: sn,
			}
			lastSN = sn
		}
	}

	// Add the last pair
	pairs = append(pairs, currentPair)

	return pairs
}

// cleanup removes old packets from the buffer
func (b *Buffer) cleanup() {
	if len(b.packets) < b.size {
		return
	}

	// Remove packets that are too old
	threshold := b.lastSequenceNumber - uint16(b.size)
	for sn := range b.packets {
		if sn < threshold {
			delete(b.packets, sn)
			delete(b.missingPackets, sn)
		}
	}
}

// Stats returns buffer statistics
func (b *Buffer) Stats() (received, lost uint64) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.packetsReceived, b.packetsLost
}

// Clear removes all packets from the buffer
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.packets = make(map[uint16]*rtp.Packet)
	b.missingPackets = make(map[uint16]time.Time)
	b.initialized = false
	b.packetsReceived = 0
	b.packetsLost = 0
}
