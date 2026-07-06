# Audio & Video Packet Issue - Root Cause Analysis

## Problem Summary
Users cannot hear or see each other - audio and video packets are not being sent between peers, even though connections appear to be established.

## Root Causes Identified

### 1. **CRITICAL: Transceiver Direction Mismatch**
**Location:** [internal/sfu/sfu.go](internal/sfu/sfu.go#L195-L220) and [peer_manager.go](internal/sfu/peer_manager.go#L138-L150)

**Issue:**
- The SFU server adds **recvonly** transceivers to receive media from each client
- When Client A publishes a track, the SFU auto-subscribes Client B
- But Client B ALSO has **recvonly** transceivers configured
- **Problem:** recvonly transceivers CANNOT receive tracks - recvonly means "receive only the offer's media, don't send any"
- To RECEIVE audio/video from the SFU, Client B needs **sendonly** or **sendrecv** transceivers

**Why this breaks:**
```
Client A → sends audio/video → SFU (receives on recvonly ✓)
SFU → wants to send A's media to Client B (uses sendonly transceiver)
Client B → has ONLY recvonly transceivers ✗ CANNOT RECEIVE ✗
```

**Evidence in code:**
[sfu.go lines 195-220] - SFU adds recvonly transceivers:
```go
videoTransceiver, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
    Direction: webrtc.RTPTransceiverDirectionRecvonly,
})
```

[peer_manager.go lines 138-150] - When adding remote tracks, it tries to use sendonly:
```go
client.Negotiator.AddTransceiverFromKind(TransceiverRequest{
    CodecType: trackToForward.Kind(),
    Init: webrtc.RTPTransceiverInit{
        Direction: webrtc.RTPTransceiverDirectionSendonly,  // ← MISMATCH!
    },
})
```

### 2. **CRITICAL: RTP Packets Are Generated But Not Delivered**
**Location:** [internal/sfu/pubsub/pubsub.go](internal/sfu/pubsub/pubsub.go#L378-L430)

**Issue:**
- RTP forwarding starts BEFORE peer has received answer OR the transceiver is properly negotiated
- The `forwardTrack()` goroutine reads and writes RTP packets to subscribers' local tracks
- But if the subscriber's peer connection doesn't have a valid sendonly transceiver for this track, the RTP.WriteRTP() silently fails or the packets are dropped

**Problem execution flow:**
1. Client A sends track → SFU receives it → starts `forwardTrack()` 
2. `forwardTrack()` immediately starts reading RTP packets
3. At the SAME time, subscription is added: `pubsub.Sub()` which calls `sub.Writer.WriteRTP()`
4. But Client B's peer connection might NOT have the necessary transceiver yet!
5. Since it's async, the WriteRTP() succeeds (no error) but the actual transport isn't ready

### 3. **Timing Issue: Renegotiation Happens Too Late**
**Location:** [sfu.go](internal/sfu/sfu.go#L390-400) - `addTrackToClient()`

**Issue:**
- Subscription happens BEFORE client has properly negotiated the new track
- The Negotiator's `AddTransceiverFromKind()` call queues renegotiation
- But there's NO GUARANTEE the new offer/answer exchange completes before RTP forwarding starts
- Race condition between RTP forwarding and offer/answer negotiation

### 4. **Missing: OnTrack Handler on Server Side**
**Location:** [transport/webrtc_transport.go](internal/transport/webrtc_transport.go#L156-L175)

**Issue:**
- The server DOES have an OnTrack handler for receiving from clients
- But the handler only logs the track, it doesn't trigger any action to notify subscribers yet
- The actual subscription happens in the PeerManager's Add() method separately
- This creates a window where tracks are received but not being forwarded to other peers

### 5. **Browser: No Proper Error Handling for Track Addition Failures**
**Location:** [frontend/videocall.tmpl](frontend/videocall.tmpl#L900-950)

**Issue:**
- When the SFU sends `pub_track` event to browser, the JavaScript should handle it
- **BUT:** Looking at the JS code, there's NO handler for `pub_track` messages in `handleSFUMessage()`!
- The browser never receives notifications about remote tracks being available

**In handleSFUMessage() [Line 1372-1377]:**
```javascript
function handleSFUMessage(msg) {
    switch (msg.type) {
        case 'offer':
            handleOffer(msg);
            break;
        case 'answer':
            handleAnswer(msg);
            break;
        case 'candidate':
            handleCandidate(msg);
            break;
        // ← NO CASE FOR 'pub_track'! Browser ignores track notification messages!
    }
}
```

## Summary of Issues

| Issue | Impact | Severity |
|-------|--------|----------|
| Transceiver direction mismatch | Remote track receiving impossible | CRITICAL |
| Missing pub_track handler in JS | Browser unaware of available tracks | CRITICAL |
| RTP forward too early | Packets lost before media negotiation complete | HIGH |
| No proper OnTrack coordination | Server doesn't coordinate forwarding properly | HIGH |
| Race condition in renegotiation | Packets arrive before transceiver ready | HIGH |

## Why No Error Appears
- Pion WebRTC silently drops RTP packets that don't have a valid sender
- Browsers silently ignore signaling messages they don't understand
- The protocol continues normally, but no media flows
- Logs say "offer sent", "answer received", successful connection - but it's all signaling, no media plane
