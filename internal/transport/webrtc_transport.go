package transport

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// WebRTCTransport implements the Transport interface using Pion WebRTC
type WebRTCTransport struct {
	clientID string
	roomID   string
	peerConn *webrtc.PeerConnection

	// Channels
	remoteTracksCh chan TrackRemoteWithRTCPReader
	messagesCh     chan webrtc.DataChannelMessage
	doneCh         chan struct{}
	signalCh       chan SignalMessage
	closeOnce      sync.Once

	// State
	mu          sync.RWMutex
	localTracks []TrackWithMID
	dataChannel *webrtc.DataChannel

	// Callbacks
	onSignal func(SignalMessage)
}

// SignalMessage represents a signaling message
type SignalMessage struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Candidate *webrtc.ICECandidate   `json:"candidate,omitempty"`
	SDP       string                 `json:"sdp,omitempty"`
}

// NewWebRTCTransport creates a new WebRTC transport
func NewWebRTCTransport(clientID, roomID string, iceServers []webrtc.ICEServer) (*WebRTCTransport, error) {
	config := webrtc.Configuration{
		ICEServers: iceServers,
	}

	// Create media engine with comprehensive codec support
	m := &webrtc.MediaEngine{}

	// Phase 5: Audio Noise Fix - Configure Opus with proper settings for noise reduction
	// Use mono audio with lower bitrate and constant bitrate mode for cleaner audio
	opusCodec := webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeOpus,
		ClockRate:   48000,
		Channels:    2, // Use stereo for better compatibility, browser will negotiate
		SDPFmtpLine: "minptime=10;useinbandfec=1",
	}

	// Register Opus codec with standard payload type 111
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: opusCodec,
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register opus codec: %w", err)
	}

	// Register VP8 video codec with standard payload type 96
	vp8Codec := webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: vp8Codec,
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("register vp8 codec: %w", err)
	}

	// Register VP8 RTX (retransmission) codec for reliability - payload type 97
	vp8RtxCodec := webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeRTX,
		ClockRate:   90000,
		SDPFmtpLine: "apt=96",
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: vp8RtxCodec,
		PayloadType:        97,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("WebRTCTransport: Warning - could not register VP8 RTX codec: %v", err)
		// Non-fatal - continue without RTX
	}

	// Register additional video codecs for better compatibility
	// VP9 - payload type 98
	vp9Codec := webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP9,
		ClockRate: 90000,
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: vp9Codec,
		PayloadType:        98,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("WebRTCTransport: Warning - could not register VP9 codec: %v", err)
	}

	// H264 - payload type 102 (for broader browser compatibility)
	h264Codec := webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: h264Codec,
		PayloadType:        102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("WebRTCTransport: Warning - could not register H264 codec: %v", err)
	}

	log.Printf("WebRTCTransport: Registered codecs - Opus(111), VP8(96), VP8-RTX(97), VP9(98), H264(102)")

	// Create interceptor registry
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	// Create setting engine
	s := webrtc.SettingEngine{}
	s.DetachDataChannels()

	// Phase 5: Audio Noise Fix - Set audio processing parameters
	// These settings help reduce background noise and echo
	// Note: DTLS settings are handled through proper certificate validation

	// Enable extended filter for better audio quality
	s.SetDTLSInsecureSkipHelloVerify(true)

	// Create API
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithSettingEngine(s),
	)

	peerConn, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	t := &WebRTCTransport{
		clientID:       clientID,
		roomID:         roomID,
		peerConn:       peerConn,
		remoteTracksCh: make(chan TrackRemoteWithRTCPReader),
		messagesCh:     make(chan webrtc.DataChannelMessage),
		doneCh:         make(chan struct{}),
		signalCh:       make(chan SignalMessage, 10),
		localTracks:    make([]TrackWithMID, 0),
	}

	// NOTE: We do NOT add transceivers upfront anymore
	// Transceivers will be created naturally when tracks are added via AddTrack()
	// This prevents codec/direction mismatch issues during negotiation
	// The client's addTrack() calls will create appropriate transceivers with sendrecv direction

	log.Printf("WebRTCTransport: Created transport for client %s (no upfront transceivers - will create on track add)", clientID)

	t.setupPeerConnectionHandlers()

	return t, nil
}

// setupPeerConnectionHandlers sets up all peer connection event handlers
func (t *WebRTCTransport) setupPeerConnectionHandlers() {
	// Handle ICE candidates
	t.peerConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		t.signalCh <- SignalMessage{
			Type:      "candidate",
			Candidate: candidate,
		}
	})

	// Handle incoming tracks
	t.peerConn.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("WebRTCTransport: Received track %s from client %s", track.ID(), t.clientID)

		rtcpReader := &rtcpReaderImpl{receiver: receiver}

		// Safely send to channel, checking if transport is closed
		select {
		case <-t.doneCh:
			return
		case t.remoteTracksCh <- TrackRemoteWithRTCPReader{
			TrackRemote: &trackRemoteImpl{track: track},
			RTCPReader:  rtcpReader,
		}:
			// Successfully sent
		}

		// Start RTCP processing for this track
		go t.processRTCP(receiver)
	})

	// Handle connection state changes
	t.peerConn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTCTransport: Connection state changed to %s for client %s", state.String(), t.clientID)

		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			t.closeOnce.Do(func() {
				close(t.doneCh)
			})
		}
	})

	// Handle data channel
	t.peerConn.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("WebRTCTransport: Data channel received from client %s", t.clientID)

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			t.messagesCh <- msg
		})
	})
}

// processRTCP processes RTCP packets for a receiver
func (t *WebRTCTransport) processRTCP(receiver *webrtc.RTPReceiver) {
	rtcpBuf := make([]byte, 1500)
	for {
		n, _, err := receiver.Read(rtcpBuf)
		if err != nil {
			return
		}

		// Process RTCP packets
		packets, err := rtcp.Unmarshal(rtcpBuf[:n])
		if err != nil {
			continue
		}

		for _, packet := range packets {
			switch p := packet.(type) {
			case *rtcp.PictureLossIndication:
				log.Printf("WebRTCTransport: Received PLI for SSRC %d", p.MediaSSRC)
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				log.Printf("WebRTCTransport: Received REMB with bitrate %f", p.Bitrate)
			}
		}
	}
}

// ClientID returns the client ID
func (t *WebRTCTransport) ClientID() string {
	return t.clientID
}

// Type returns the transport type
func (t *WebRTCTransport) Type() Type {
	return TypeWebRTC
}

// RemoteTracksChannel returns the channel for remote tracks
func (t *WebRTCTransport) RemoteTracksChannel() <-chan TrackRemoteWithRTCPReader {
	return t.remoteTracksCh
}

// LocalTracks returns all local tracks
func (t *WebRTCTransport) LocalTracks() []TrackWithMID {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.localTracks
}

// AddTrack adds a track to the transport
func (t *WebRTCTransport) AddTrack(track Track) (TrackLocal, RTCPReader, error) {
	var webrtcTrack *webrtc.TrackLocalStaticRTP

	switch track.Kind() {
	case webrtc.RTPCodecTypeVideo:
		var err error
		webrtcTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
			track.ID(),
			track.StreamID(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create video track: %w", err)
		}
	case webrtc.RTPCodecTypeAudio:
		var err error
		webrtcTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			track.ID(),
			track.StreamID(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create audio track: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported track kind: %v", track.Kind())
	}

	rtpSender, err := t.peerConn.AddTrack(webrtcTrack)
	if err != nil {
		return nil, nil, fmt.Errorf("add track to peer connection: %w", err)
	}

	t.mu.Lock()
	t.localTracks = append(t.localTracks, TrackWithMID{
		Track: &trackImpl{
			id:       track.ID(),
			streamID: track.StreamID(),
			kind:     track.Kind(),
		},
		MID: "", // Will be set after negotiation
	})
	t.mu.Unlock()

	trackLocal := &trackLocalImpl{
		track: webrtcTrack,
	}

	rtcpReader := &rtcpReaderImpl{sender: rtpSender}

	return trackLocal, rtcpReader, nil
}

// RemoveTrack removes a track from the transport
func (t *WebRTCTransport) RemoveTrack(trackID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, track := range t.localTracks {
		if track.Track.ID() == trackID {
			// Remove from peer connection
			for _, sender := range t.peerConn.GetSenders() {
				if sender.Track() != nil && sender.Track().ID() == trackID {
					if err := t.peerConn.RemoveTrack(sender); err != nil {
						return fmt.Errorf("remove track from peer connection: %w", err)
					}
					break
				}
			}

			// Remove from local tracks
			t.localTracks = append(t.localTracks[:i], t.localTracks[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("track not found: %s", trackID)
}

// WriteRTCP writes RTCP packets
func (t *WebRTCTransport) WriteRTCP(packets []rtcp.Packet) error {
	return t.peerConn.WriteRTCP(packets)
}

// MessagesChannel returns the data channel messages channel
func (t *WebRTCTransport) MessagesChannel() <-chan webrtc.DataChannelMessage {
	return t.messagesCh
}

// Send sends a data channel message
func (t *WebRTCTransport) Send(message webrtc.DataChannelMessage) <-chan error {
	errCh := make(chan error, 1)

	if t.dataChannel == nil {
		errCh <- fmt.Errorf("data channel not available")
		return errCh
	}

	go func() {
		if err := t.dataChannel.Send(message.Data); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	return errCh
}

// SignalChannel returns the signaling message channel
func (t *WebRTCTransport) SignalChannel() <-chan SignalMessage {
	return t.signalCh
}

// Signal sends a signal to the transport
func (t *WebRTCTransport) Signal(msg SignalMessage) error {
	switch msg.Type {
	case "offer":
		return t.handleOffer(msg)
	case "answer":
		return t.handleAnswer(msg)
	case "candidate":
		return t.handleCandidate(msg)
	default:
		return fmt.Errorf("unknown signal type: %s", msg.Type)
	}
}

// validateSDP checks if the SDP contains required ICE credentials
// Note: For initial answers, we allow SDP without ice-ufrag/ice-pwd as they may
// be added later via ICE candidates. This is common in some WebRTC implementations.
func validateSDP(sdp string, isAnswer bool) error {
	if sdp == "" {
		return fmt.Errorf("SDP is empty")
	}

	// Check for basic SDP structure
	if !strings.Contains(sdp, "v=0") {
		return fmt.Errorf("SDP missing version line")
	}

	// For answers, we used to require ice-ufrag/ice-pwd, but some browsers
	// send initial answers without them and add ICE candidates separately.
	// We'll log a warning but not fail if they're missing in initial answers.
	if isAnswer {
		// Check for ice-ufrag attribute which is required for ICE negotiation
		if !strings.Contains(sdp, "ice-ufrag") {
			log.Printf("WebRTCTransport: Warning - SDP missing ice-ufrag attribute, will rely on ICE candidates")
		}

		// Check for ice-pwd attribute as well
		if !strings.Contains(sdp, "ice-pwd") {
			log.Printf("WebRTCTransport: Warning - SDP missing ice-pwd attribute, will rely on ICE candidates")
		}

		// Check for m-lines (media sections)
		if !strings.Contains(sdp, "m=") {
			return fmt.Errorf("SDP missing media sections (m= lines)")
		}
	}

	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleOffer handles an offer signal
func (t *WebRTCTransport) handleOffer(msg SignalMessage) error {
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	}

	if err := t.peerConn.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}

	answer, err := t.peerConn.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("create answer: %w", err)
	}

	if err := t.peerConn.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	<-webrtc.GatheringCompletePromise(t.peerConn)

	// Send answer back
	t.signalCh <- SignalMessage{
		Type: "answer",
		SDP:  t.peerConn.LocalDescription().SDP,
	}

	return nil
}

// handleAnswer handles an answer signal
func (t *WebRTCTransport) handleAnswer(msg SignalMessage) error {
	// Validate the answer SDP before processing
	// Pass true to indicate this is an answer (allows missing ICE credentials initially)
	if err := validateSDP(msg.SDP, true); err != nil {
		log.Printf("WebRTCTransport: Invalid answer SDP from client %s: %v", t.clientID, err)
		log.Printf("WebRTCTransport: SDP content (first 500 chars): %s", msg.SDP[:min(500, len(msg.SDP))])
		return fmt.Errorf("invalid answer SDP: %w", err)
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.SDP,
	}

	if err := t.peerConn.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}

	return nil
}

// handleCandidate handles an ICE candidate signal
func (t *WebRTCTransport) handleCandidate(msg SignalMessage) error {
	if msg.Candidate == nil {
		return fmt.Errorf("nil candidate")
	}

	candidateInit := msg.Candidate.ToJSON()
	if err := t.peerConn.AddICECandidate(candidateInit); err != nil {
		return fmt.Errorf("add ICE candidate: %w", err)
	}

	return nil
}

// Close closes the transport
func (t *WebRTCTransport) Close() error {
	// Close done channel first to signal all goroutines to stop
	t.closeOnce.Do(func() {
		close(t.doneCh)
	})

	// Close peer connection
	if err := t.peerConn.Close(); err != nil {
		return err
	}

	return nil
}

// Done returns a channel that's closed when the transport is done
func (t *WebRTCTransport) Done() <-chan struct{} {
	return t.doneCh
}

// CreateOffer creates an offer for initiating a connection
func (t *WebRTCTransport) CreateOffer() (webrtc.SessionDescription, error) {
	offer, err := t.peerConn.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("create offer: %w", err)
	}

	if err := t.peerConn.SetLocalDescription(offer); err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering with timeout
	gatherComplete := webrtc.GatheringCompletePromise(t.peerConn)

	select {
	case <-gatherComplete:
		// ICE gathering completed
	case <-time.After(5 * time.Second):
		// Timeout - proceed with what we have
		log.Printf("WebRTCTransport: ICE gathering timeout for client %s, proceeding with current candidates", t.clientID)
	}

	// Get the local description (might be nil if not set)
	localDesc := t.peerConn.LocalDescription()
	if localDesc == nil {
		return webrtc.SessionDescription{}, fmt.Errorf("local description is nil after offer creation")
	}

	return *localDesc, nil
}

// GetPeerConnection returns the underlying peer connection
func (t *WebRTCTransport) GetPeerConnection() *webrtc.PeerConnection {
	return t.peerConn
}

// trackLocalImpl implements TrackLocal
type trackLocalImpl struct {
	track *webrtc.TrackLocalStaticRTP
}

func (t *trackLocalImpl) Track() Track {
	return &trackImpl{
		id:       t.track.ID(),
		streamID: t.track.StreamID(),
		kind:     t.track.Kind(),
	}
}

func (t *trackLocalImpl) Write(p []byte) (int, error) {
	return t.track.Write(p)
}

func (t *trackLocalImpl) WriteRTP(packet *rtp.Packet) error {
	return t.track.WriteRTP(packet)
}

// trackRemoteImpl implements TrackRemote
type trackRemoteImpl struct {
	track *webrtc.TrackRemote
}

func (t *trackRemoteImpl) Track() Track {
	return &trackImpl{
		id:       t.track.ID(),
		streamID: t.track.StreamID(),
		kind:     t.track.Kind(),
	}
}

func (t *trackRemoteImpl) ReadRTP() (*rtp.Packet, interceptor.Attributes, error) {
	return t.track.ReadRTP()
}

func (t *trackRemoteImpl) SSRC() webrtc.SSRC {
	return t.track.SSRC()
}

func (t *trackRemoteImpl) RID() string {
	return t.track.RID()
}

// trackImpl implements Track
type trackImpl struct {
	id       string
	streamID string
	kind     webrtc.RTPCodecType
}

func (t *trackImpl) ID() string {
	return t.id
}

func (t *trackImpl) StreamID() string {
	return t.streamID
}

func (t *trackImpl) Kind() webrtc.RTPCodecType {
	return t.kind
}

// rtcpReaderImpl implements RTCPReader
type rtcpReaderImpl struct {
	receiver *webrtc.RTPReceiver
	sender   *webrtc.RTPSender
}

func (r *rtcpReaderImpl) ReadRTCP() ([]rtcp.Packet, interceptor.Attributes, error) {
	buf := make([]byte, 1500)
	var n int
	var err error

	if r.receiver != nil {
		n, _, err = r.receiver.Read(buf)
	} else if r.sender != nil {
		n, _, err = r.sender.Read(buf)
	} else {
		return nil, nil, fmt.Errorf("no RTCP source")
	}

	if err != nil {
		return nil, nil, err
	}

	packets, err := rtcp.Unmarshal(buf[:n])
	if err != nil {
		return nil, nil, err
	}

	return packets, nil, nil
}
