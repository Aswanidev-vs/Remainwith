package jitter

import (
	"github.com/pion/rtcp"
)

// NackPair represents a range of missing packets
type NackPair struct {
	PacketID    uint16
	LostPackets uint16 // Bitmask of lost packets
}

// CreateNackPair creates a NACK pair from a list of missing sequence numbers
func CreateNackPair(missingSNs []uint16) rtcp.NackPair {
	if len(missingSNs) == 0 {
		return rtcp.NackPair{}
	}

	// Sort the sequence numbers
	sorted := make([]uint16, len(missingSNs))
	copy(sorted, missingSNs)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Create NACK pair
	pair := rtcp.NackPair{
		PacketID: sorted[0],
	}

	// Set bits for subsequent missing packets within 16 packets of the first
	for i := 1; i < len(sorted) && i <= 16; i++ {
		diff := sorted[i] - sorted[0]
		if diff > 0 && diff <= 16 {
			pair.LostPackets |= 1 << (diff - 1)
		}
	}

	return pair
}

// CreateNackPairs creates multiple NACK pairs from a list of missing sequence numbers
func CreateNackPairs(missingSNs []uint16) []rtcp.NackPair {
	if len(missingSNs) == 0 {
		return nil
	}

	var pairs []rtcp.NackPair
	var current []uint16

	for _, sn := range missingSNs {
		if len(current) == 0 {
			current = append(current, sn)
			continue
		}

		// Check if this SN can be included in the current pair
		last := current[len(current)-1]
		if sn == last+1 && len(current) < 17 {
			current = append(current, sn)
		} else {
			// Start a new pair
			pairs = append(pairs, CreateNackPair(current))
			current = []uint16{sn}
		}
	}

	// Add the last pair
	if len(current) > 0 {
		pairs = append(pairs, CreateNackPair(current))
	}

	return pairs
}

// ParseNackPairs extracts individual sequence numbers from NACK pairs
func ParseNackPairs(pairs []rtcp.NackPair) []uint16 {
	var sns []uint16

	for _, pair := range pairs {
		// Add the base sequence number
		sns = append(sns, pair.PacketID)

		// Check each bit in the lost packets mask
		for i := 0; i < 16; i++ {
			if pair.LostPackets&(1<<i) != 0 {
				sns = append(sns, pair.PacketID+uint16(i+1))
			}
		}
	}

	return sns
}

// ShouldSendNack determines if a NACK should be sent based on the number of
// missing packets and the time since the last NACK
func ShouldSendNack(missingCount int, lastNackAgeMs int64) bool {
	// Send NACK if we have enough missing packets
	if missingCount < NackThreshold {
		return false
	}

	// Don't send NACKs too frequently (max 1 per 10ms)
	if lastNackAgeMs < 10 {
		return false
	}

	return true
}
