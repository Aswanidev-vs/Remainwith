# Video Packet Fix Implementation Plan

## Problem
Video packets are not being seen by other users in the WebRTC video call.

## Root Causes Identified
1. **Transceiver Direction Mismatch**: SFU creates `recvonly` transceivers, but client needs `sendrecv`
2. **Missing Video Codec Registration**: Only VP8 registered, browsers may use VP9/H264
3. **Track Forwarding Logic**: Need to verify video track subscriptions work correctly
4. **Client-Side Track Timing**: Tracks added after offer causes negotiation issues

## Implementation Steps

### [x] Step 1: Fix SFU Transceiver Direction (internal/sfu/sfu.go)
- Changed `recvonly` to `sendrecv` for both audio and video transceivers
- This enables bidirectional media flow

### [x] Step 2: Add Video Codec Support (internal/transport/webrtc_transport.go)
- VP9 and H264 codecs already registered alongside VP8
- Broader browser compatibility ensured

### [x] Step 3: Add Video Forwarding Debug (internal/sfu/pubsub/pubsub.go)
- Added logging to track video packet forwarding every 5 seconds
- Logs show packet count and subscriber count for video tracks

### [x] Step 4: Fix Client Track Timing (frontend/videocall.tmpl)
- Added tracks using addTransceiver with sendrecv direction BEFORE setting remote description
- This ensures proper bidirectional media flow negotiation


## Testing Checklist
- [ ] Audio still works after changes
- [ ] Video packets flow from publisher to SFU
- [ ] Video packets flow from SFU to subscribers
- [ ] All participants can see each other's video
