package jitter

import (
	"log"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// JitterHandler is the interface for handling jitter buffer operations
type JitterHandler interface {
	// HandleNack processes a NACK packet and returns any recovered packets
	// and a NACK to forward if packets couldn't be recovered
	HandleNack(nack *rtcp.TransportLayerNack) ([]*rtp.Packet, *rtcp.TransportLayerNack)

	// HandleRTP processes an incoming RTP packet and returns a NACK if needed
	HandleRTP(pkt *rtp.Packet) rtcp.Packet

	// RemoveBuffer removes the buffer for a specific SSRC
	RemoveBuffer(ssrc uint32)
}

// NackHandler implements JitterHandler with actual NACK processing
type NackHandler struct {
	jitterBuffer *JitterBuffer
	enabled      bool
}

// NewNackHandler creates a new NACK handler with jitter buffer
func NewNackHandler(enabled bool) *NackHandler {
	var jb *JitterBuffer
	if enabled {
		jb = NewJitterBuffer()
	}

	return &NackHandler{
		jitterBuffer: jb,
		enabled:      enabled,
	}
}

// NewJitterHandler creates a JitterHandler (either real or noop)
func NewJitterHandler(enabled bool) JitterHandler {
	if enabled {
		return NewNackHandler(true)
	}
	return &NoopNackHandler{}
}

// HandleNack processes a NACK packet
func (n *NackHandler) HandleNack(nack *rtcp.TransportLayerNack) ([]*rtp.Packet, *rtcp.TransportLayerNack) {
	if !n.enabled || n.jitterBuffer == nil {
		return nil, nack
	}

	ssrc := nack.MediaSSRC
	var rtpPackets []*rtp.Packet
	var actualNacks []rtcp.NackPair

	// Process each NACK pair
	for _, nackPair := range nack.Nacks {
		// Get the list of missing packets
		missingSNs := ParseNackPairs([]rtcp.NackPair{nackPair})

		log.Printf("NACK received for SSRC %d: %d packets", ssrc, len(missingSNs))

		notFound := make([]uint16, 0, len(missingSNs))

		// Try to find each missing packet in the jitter buffer
		for _, sn := range missingSNs {
			rtpPacket := n.jitterBuffer.GetPacket(ssrc, sn)
			if rtpPacket == nil {
				// Packet not found in jitter buffer
				notFound = append(notFound, sn)
				log.Printf("  Packet %d not found in buffer", sn)
			} else {
				// Found the missing packet
				rtpPackets = append(rtpPackets, rtpPacket)
				log.Printf("  Packet %d found in buffer", sn)
			}
		}

		// If some packets weren't found, create a new NACK for those
		if len(notFound) > 0 {
			actualNacks = append(actualNacks, CreateNackPairs(notFound)...)
		}
	}

	// If we found all packets, return them with no NACK to forward
	if len(actualNacks) == 0 {
		return rtpPackets, nil
	}

	// Return recovered packets and a NACK for packets we couldn't find
	return rtpPackets, &rtcp.TransportLayerNack{
		MediaSSRC:  nack.MediaSSRC,
		SenderSSRC: nack.SenderSSRC,
		Nacks:      actualNacks,
	}
}

// HandleRTP processes an incoming RTP packet
func (n *NackHandler) HandleRTP(pkt *rtp.Packet) rtcp.Packet {
	if !n.enabled || n.jitterBuffer == nil {
		return nil
	}

	return n.jitterBuffer.PushRTP(pkt)
}

// RemoveBuffer removes the buffer for a specific SSRC
func (n *NackHandler) RemoveBuffer(ssrc uint32) {
	if n.jitterBuffer != nil {
		n.jitterBuffer.RemoveBuffer(ssrc)
	}
}

// GetStats returns statistics for all buffers
func (n *NackHandler) GetStats() map[uint32]struct {
	Received uint64
	Lost     uint64
} {
	if n.jitterBuffer == nil {
		return nil
	}
	return n.jitterBuffer.Stats()
}

// NoopNackHandler is a no-op implementation of JitterHandler
type NoopNackHandler struct{}

// HandleNack does nothing
func (n *NoopNackHandler) HandleNack(nack *rtcp.TransportLayerNack) ([]*rtp.Packet, *rtcp.TransportLayerNack) {
	// Pass through the NACK without processing
	return nil, nack
}

// HandleRTP does nothing
func (n *NoopNackHandler) HandleRTP(pkt *rtp.Packet) rtcp.Packet {
	return nil
}

// RemoveBuffer does nothing
func (n *NoopNackHandler) RemoveBuffer(ssrc uint32) {}
