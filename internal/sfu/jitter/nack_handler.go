// Package jitter provides NACK (Negative Acknowledgment) handling for packet loss recovery
package jitter

import (
	"log"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// NACKHandler handles Negative Acknowledgment for packet loss recovery
type NACKHandler struct {
	mu sync.RWMutex

	// Packet history for retransmission
	packetHistory map[uint16]*rtp.Packet
	historySize   int
	minSeq        uint16
	maxSeq        uint16

	// NACK tracking
	nackCount    map[uint16]int
	maxNackCount int
	lastNACKTime map[uint16]time.Time
	nackInterval time.Duration

	// Callbacks
	onNACK func([]uint16)

	// Control
	closed bool
}

// NACKConfig contains NACK handler configuration
type NACKConfig struct {
	HistorySize  int
	MaxNACKCount int
	NACKInterval time.Duration
}

// DefaultNACKConfig returns default NACK configuration
func DefaultNACKConfig() NACKConfig {
	return NACKConfig{
		HistorySize:  500,                   // Keep last 500 packets
		MaxNACKCount: 10,                    // Max 10 NACKs per packet
		NACKInterval: 20 * time.Millisecond, // Wait 20ms between NACKs
	}
}

// NewNACKHandler creates a new NACK handler
func NewNACKHandler(config NACKConfig) *NACKHandler {
	return &NACKHandler{
		packetHistory: make(map[uint16]*rtp.Packet),
		historySize:   config.HistorySize,
		nackCount:     make(map[uint16]int),
		maxNackCount:  config.MaxNACKCount,
		lastNACKTime:  make(map[uint16]time.Time),
		nackInterval:  config.NACKInterval,
	}
}

// StorePacket stores a packet in history for potential retransmission
func (nh *NACKHandler) StorePacket(packet *rtp.Packet) {
	nh.mu.Lock()
	defer nh.mu.Unlock()

	if nh.closed {
		return
	}

	seq := packet.SequenceNumber

	// Store packet
	nh.packetHistory[seq] = packet

	// Update sequence number range
	if len(nh.packetHistory) == 1 {
		nh.minSeq = seq
		nh.maxSeq = seq
	} else {
		if seqLess(seq, nh.minSeq) {
			nh.minSeq = seq
		}
		if seqLess(nh.maxSeq, seq) {
			nh.maxSeq = seq
		}
	}

	// Clean up old packets if history is too large
	if len(nh.packetHistory) > nh.historySize {
		nh.cleanupOldPackets()
	}
}

// GetRetransmission retrieves a packet for retransmission
func (nh *NACKHandler) GetRetransmission(seq uint16) *rtp.Packet {
	nh.mu.RLock()
	defer nh.mu.RUnlock()

	return nh.packetHistory[seq]
}

// HandleNACK processes incoming NACK packets from subscribers
func (nh *NACKHandler) HandleNACK(nack *rtcp.TransportLayerNack) []uint16 {
	nh.mu.Lock()
	defer nh.mu.Unlock()

	if nh.closed {
		return nil
	}

	var missingSeqs []uint16

	// Process each NACK item
	for _, item := range nack.Nacks {
		// Decode the sequence numbers from the NACK item
		seqs := decodeNACKItem(item)

		for _, seq := range seqs {
			// Check if we have this packet
			if _, ok := nh.packetHistory[seq]; !ok {
				// We don't have this packet, add to missing list
				missingSeqs = append(missingSeqs, seq)
			}
		}
	}

	return missingSeqs
}

// GenerateNACK generates NACK packets for missing sequences
func (nh *NACKHandler) GenerateNACK(missingSeqs []uint16) *rtcp.TransportLayerNack {
	nh.mu.Lock()
	defer nh.mu.Unlock()

	if nh.closed || len(missingSeqs) == 0 {
		return nil
	}

	now := time.Now()
	var nackItems []rtcp.NackPair

	for _, seq := range missingSeqs {
		// Check if we should send NACK for this sequence
		lastNACK, ok := nh.lastNACKTime[seq]
		if ok && now.Sub(lastNACK) < nh.nackInterval {
			// Too soon, skip
			continue
		}

		// Check NACK count
		count := nh.nackCount[seq]
		if count >= nh.maxNackCount {
			// Too many NACKs, give up
			continue
		}

		// Update tracking
		nh.nackCount[seq] = count + 1
		nh.lastNACKTime[seq] = now

		// Add to NACK items (simplified - just add individual sequences)
		// In production, batch consecutive sequences into NackPair
		nackItems = append(nackItems, rtcp.NackPair{
			PacketID: seq,
		})
	}

	if len(nackItems) == 0 {
		return nil
	}

	return &rtcp.TransportLayerNack{
		MediaSSRC: 0, // Will be set by caller
		Nacks:     nackItems,
	}
}

// ProcessIncomingNACK processes a NACK from a remote peer and returns packets to retransmit
func (nh *NACKHandler) ProcessIncomingNACK(nack *rtcp.TransportLayerNack) []*rtp.Packet {
	nh.mu.RLock()
	defer nh.mu.RUnlock()

	var packets []*rtp.Packet

	for _, item := range nack.Nacks {
		seqs := decodeNACKItem(item)
		for _, seq := range seqs {
			if packet, ok := nh.packetHistory[seq]; ok {
				// Clone packet for retransmission
				clone := &rtp.Packet{
					Header:  packet.Header,
					Payload: make([]byte, len(packet.Payload)),
				}
				copy(clone.Payload, packet.Payload)
				packets = append(packets, clone)
			}
		}
	}

	return packets
}

// cleanupOldPackets removes old packets from history
func (nh *NACKHandler) cleanupOldPackets() {
	// Remove packets that are too old (below minSeq + some threshold)
	threshold := nh.minSeq + uint16(nh.historySize/2)

	for seq := range nh.packetHistory {
		if seqLess(seq, threshold) {
			delete(nh.packetHistory, seq)
			delete(nh.nackCount, seq)
			delete(nh.lastNACKTime, seq)
		}
	}
}

// Clear clears all stored packets
func (nh *NACKHandler) Clear() {
	nh.mu.Lock()
	defer nh.mu.Unlock()

	nh.packetHistory = make(map[uint16]*rtp.Packet)
	nh.nackCount = make(map[uint16]int)
	nh.lastNACKTime = make(map[uint16]time.Time)
	nh.minSeq = 0
	nh.maxSeq = 0
}

// Close closes the NACK handler
func (nh *NACKHandler) Close() {
	nh.mu.Lock()
	defer nh.mu.Unlock()

	nh.closed = true
	nh.Clear()
}

// seqLess compares two sequence numbers with wraparound handling
func seqLess(a, b uint16) bool {
	// Handle wraparound: if a is much larger than b, it wrapped around
	diff := int16(a - b)
	return diff < 0
}

// decodeNACKItem decodes a NACK item into individual sequence numbers
func decodeNACKItem(item rtcp.NackPair) []uint16 {
	var seqs []uint16

	// PacketID is the first missing sequence
	seqs = append(seqs, item.PacketID)

	// LostPackets is a bitmask of subsequent lost packets
	// Bit 0 = PacketID + 1, Bit 1 = PacketID + 2, etc.
	for i := 0; i < 16; i++ {
		if item.LostPackets&(1<<i) != 0 {
			seqs = append(seqs, item.PacketID+uint16(i+1))
		}
	}

	return seqs
}

// encodeNACKItem encodes sequence numbers into NACK items (for batching)
func encodeNACKItem(seqs []uint16) []rtcp.NackPair {
	if len(seqs) == 0 {
		return nil
	}

	var items []rtcp.NackPair

	// Group consecutive sequences
	start := seqs[0]
	var lostPackets uint16

	for i := 1; i < len(seqs); i++ {
		diff := seqs[i] - start
		if diff <= 16 {
			// Within range, add to bitmask
			lostPackets |= 1 << (diff - 1)
		} else {
			// Too far, start new item
			items = append(items, rtcp.NackPair{
				PacketID:    start,
				LostPackets: rtcp.PacketBitmap(lostPackets),
			})
			start = seqs[i]
			lostPackets = 0
		}

	}

	// Add final item
	items = append(items, rtcp.NackPair{
		PacketID:    start,
		LostPackets: rtcp.PacketBitmap(lostPackets),
	})

	return items
}

// GetStats returns NACK statistics
func (nh *NACKHandler) GetStats() (storedPackets int, nackCounts map[uint16]int) {
	nh.mu.RLock()
	defer nh.mu.RUnlock()

	counts := make(map[uint16]int)
	for seq, count := range nh.nackCount {
		counts[seq] = count
	}

	return len(nh.packetHistory), counts
}

// LogStats logs NACK statistics for debugging
func (nh *NACKHandler) LogStats() {
	stored, counts := nh.GetStats()
	log.Printf("NACK Handler: %d packets stored, %d sequences with NACKs", stored, len(counts))
}
