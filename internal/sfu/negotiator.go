package sfu

import (
	"log"
	"sync"

	"github.com/pion/webrtc/v4"
)

// TransceiverRequest represents a request to add a transceiver
type TransceiverRequest struct {
	CodecType webrtc.RTPCodecType
	Init      webrtc.RTPTransceiverInit
}

// Negotiator handles WebRTC renegotiation following the peer-calls pattern
// This ensures proper sequencing of offers/answers and transceiver management
type Negotiator struct {
	initiator            bool
	peerConnection       *webrtc.PeerConnection
	onOffer              func(webrtc.SessionDescription, error)
	onRequestNegotiation func()

	negotiationDone   chan struct{}
	mu                sync.Mutex
	queuedNegotiation bool

	queuedTransceiverRequests []TransceiverRequest
}

// NewNegotiator creates a new negotiator
func NewNegotiator(
	initiator bool,
	peerConnection *webrtc.PeerConnection,
	onOffer func(webrtc.SessionDescription, error),
	onRequestNegotiation func(),
) *Negotiator {
	n := &Negotiator{
		initiator:            initiator,
		peerConnection:       peerConnection,
		onOffer:              onOffer,
		onRequestNegotiation: onRequestNegotiation,
	}

	peerConnection.OnSignalingStateChange(n.handleSignalingStateChange)
	return n
}

// AddTransceiverFromKind queues a transceiver request and triggers negotiation
func (n *Negotiator) AddTransceiverFromKind(t TransceiverRequest) {
	log.Printf("Negotiator: Add transceiver - kind=%v, direction=%v", t.CodecType, t.Init.Direction)

	n.mu.Lock()
	n.queuedTransceiverRequests = append(n.queuedTransceiverRequests, t)
	n.mu.Unlock()

	log.Printf("Negotiator: Negotiate because a transceiver was queued")
	n.Negotiate()
}

// closeDoneChannel closes the negotiation done channel safely
func (n *Negotiator) closeDoneChannel() {
	if n.negotiationDone != nil {
		close(n.negotiationDone)
		n.negotiationDone = nil
	}
}

// Done returns a channel that's closed when negotiation is complete
func (n *Negotiator) Done() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.negotiationDone != nil {
		return n.negotiationDone
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

// handleSignalingStateChange handles changes in the signaling state
func (n *Negotiator) handleSignalingStateChange(state webrtc.SignalingState) {
	log.Printf("Negotiator: Signaling state changed to %v", state)

	n.mu.Lock()
	defer n.mu.Unlock()

	switch state {
	case webrtc.SignalingStateClosed:
		n.closeDoneChannel()
	case webrtc.SignalingStateStable:
		if n.queuedNegotiation {
			log.Printf("Negotiator: Execute queued negotiation")
			n.queuedNegotiation = false
			n.negotiate()
		} else {
			n.closeDoneChannel()
		}
	}
}

// Negotiate starts a negotiation (creates offer if initiator, requests negotiation if not)
func (n *Negotiator) Negotiate() (done <-chan struct{}) {
	log.Printf("Negotiator: Negotiate called")

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.negotiationDone != nil {
		log.Printf("Negotiator: Already negotiating, queueing for later")
		n.queuedNegotiation = true
		return
	}

	log.Printf("Negotiator: Starting negotiation")
	n.negotiationDone = make(chan struct{})

	n.negotiate()
	return n.negotiationDone
}

// addQueuedTransceivers adds any queued transceivers to the peer connection
func (n *Negotiator) addQueuedTransceivers() {
	for _, t := range n.queuedTransceiverRequests {
		log.Printf("Negotiator: Adding queued transceiver - kind=%v, direction=%v", t.CodecType, t.Init.Direction)

		_, err := n.peerConnection.AddTransceiverFromKind(t.CodecType, t.Init)
		if err != nil {
			log.Printf("Negotiator: Error adding queued transceiver: %v", err)
		}
	}

	n.queuedTransceiverRequests = []TransceiverRequest{}
}

// negotiate performs the actual negotiation
func (n *Negotiator) negotiate() {
	n.addQueuedTransceivers()

	if !n.initiator {
		log.Printf("Negotiator: Requesting negotiation from initiator")
		n.requestNegotiation()
		return
	}

	log.Printf("Negotiator: Creating offer")

	offer, err := n.peerConnection.CreateOffer(nil)

	n.onOffer(offer, err)
}

// requestNegotiation requests the initiator to start negotiation
func (n *Negotiator) requestNegotiation() {
	n.onRequestNegotiation()
}
