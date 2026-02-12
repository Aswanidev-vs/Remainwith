// Package transport provides abstractions for WebRTC transport layer
package transport

import (
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Type represents the type of transport
type Type int

const (
	// TypeWebRTC represents a WebRTC peer connection transport
	TypeWebRTC Type = iota + 1
	// TypeServer represents a server-to-server transport
	TypeServer
)

// Transport defines the interface for media transport
type Transport interface {
	// ClientID returns the unique identifier for this transport's client
	ClientID() string
	// RoomID returns the room identifier for this transport
	RoomID() string
	// Type returns the transport type
	Type() Type

	DataTransport

	// RemoteTracksChannel returns a channel for incoming remote tracks
	// Use Done() in select when reading to prevent deadlocks
	RemoteTracksChannel() <-chan TrackRemoteWithRTCPReader

	// LocalTracks returns all local tracks
	LocalTracks() []TrackWithMID

	// AddTrack adds a track to the transport
	AddTrack(Track) (TrackLocal, RTCPReader, error)
	// RemoveTrack removes a track from the transport
	RemoveTrack(trackID string) error

	RTCPWriter

	Closable
}

// Closable defines the interface for closable resources
type Closable interface {
	// Close closes the transport
	Close() error
	// Done returns a channel that's closed when transport is done
	Done() <-chan struct{}
}

// trackCommon defines common track methods
type trackCommon interface {
	Track() Track
}

// TrackLocal represents a local track that can be written to
type TrackLocal interface {
	trackCommon
	Write([]byte) (int, error)
	WriteRTP(*rtp.Packet) error
}

// TrackRemote represents a remote track that can be read from
type TrackRemote interface {
	trackCommon
	ReadRTP() (*rtp.Packet, interceptor.Attributes, error)
	SSRC() webrtc.SSRC
	RID() string
}

// RTCPReader reads RTCP packets
type RTCPReader interface {
	ReadRTCP() ([]rtcp.Packet, interceptor.Attributes, error)
}

// TrackRemoteWithRTCPReader combines a remote track with its RTCP reader
type TrackRemoteWithRTCPReader struct {
	TrackRemote TrackRemote
	RTCPReader  RTCPReader
}

// RTCPWriter writes RTCP packets
type RTCPWriter interface {
	WriteRTCP([]rtcp.Packet) error
}

// DataTransport handles data channel messages
type DataTransport interface {
	// MessagesChannel returns a channel for incoming data channel messages
	MessagesChannel() <-chan webrtc.DataChannelMessage
	// Send sends a data channel message
	Send(message webrtc.DataChannelMessage) <-chan error
}

// Track represents a media track
type Track interface {
	// ID returns the track ID
	ID() string
	// StreamID returns the stream ID
	StreamID() string
	// Kind returns the track kind (audio/video)
	Kind() webrtc.RTPCodecType
}

// TrackWithMID represents a track with its MID (Media ID)
type TrackWithMID struct {
	Track Track
	MID   string
}
