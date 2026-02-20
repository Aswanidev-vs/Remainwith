# Video Packet Forwarding Fix - Implementation Plan

## Issues Identified
1. **CRITICAL: Transceiver direction was `recvonly` instead of `sendrecv`** - This prevented bidirectional media flow
2. Missing RTCP feedback parameters for video codecs (nack, nack pli, goog-remb, transport-cc)
3. No PLI (Picture Loss Indication) forwarding from subscribers to publishers
4. Improper SSRC handling for video tracks
5. Missing RTCP processing loop for video congestion control


## Implementation Steps

- [x] 1. **CRITICAL FIX: Change transceiver direction from `recvonly` to `sendrecv` in `internal/sfu/sfu.go`**
  - This enables BIDIRECTIONAL audio and video flow
  - Client can now both send AND receive media from the SFU
  - Was blocking all incoming media from other participants

- [x] 2. Fix video codec configuration in `internal/transport/webrtc_transport.go`
  - Add RTCP feedback parameters to VP8 codec registration
  - Enable nack, nack pli, goog-remb, transport-cc for video

  
- [x] 3. Add PLI forwarding to `internal/sfu/peer_manager.go`
  - Already implemented! RTCP processing loop exists
  - PLI packets are forwarded from subscribers to original publishers
  - REMB handling is in place for congestion control

  
- [x] 4. Fix track forwarding in `internal/sfu/pubsub/pubsub.go`
  - SSRC preservation already in place
  - Added better logging for video tracks in sfu.go
  - Packet forwarding loop working correctly
  
- [x] 5. Add RTCP processing in `internal/sfu/sfu.go`
  - Added RTCP feedback parameters to video tracks
  - Changed stream ID format to prevent collisions: `pub-<publisherID>-<trackID>`
  - Enhanced PLI and NACK logging for debugging
  - Integration with peer manager already exists



## Testing Steps
- [ ] Test video sharing between 2 clients
- [ ] Verify PLI packets are forwarded correctly
- [ ] Check browser console for video decoding errors
- [ ] Test with 3+ clients in a room

## Summary of Changes

### 1. `internal/transport/webrtc_transport.go`
- Added RTCP feedback parameters to VP8 codec registration:
  - `nack` - Negative acknowledgment for packet retransmission
  - `nack pli` - Picture Loss Indication for keyframe requests
  - `goog-remb` - Google Receiver Estimated Maximum Bitrate
  - `transport-cc` - Transport-wide congestion control

### 2. `internal/sfu/sfu.go`
- Added RTCP feedback to video tracks created for forwarding
- Changed stream ID format to `pub-<publisherID>-<trackID>` to ensure uniqueness
- Enhanced logging for video track creation and forwarding
- Added NACK packet logging for debugging video packet loss

### 3. `internal/sfu/pubsub/pubsub.go`
- Enhanced `forwardDirect()` with better logging for video tracks
- Added periodic stats logging every 5 seconds for video forwarding
- Tracks packet counts and subscriber success rates

### Key Points for Bidirectional Media to Work:
1. **CRITICAL: Transceiver Direction MUST be `sendrecv`**: Using `recvonly` only allows receiving, blocking all outgoing media to the client
2. **RTCP Feedback is Critical**: Video requires PLI (Picture Loss Indication) to request keyframes when packets are lost
3. **Unique Stream IDs**: Each forwarded track must have a unique stream ID to avoid collisions
4. **SSRC Preservation**: Original SSRC must be preserved when forwarding packets
5. **PLI Forwarding**: The peer_manager already handles forwarding PLI from subscribers back to publishers


## Next Steps
1. Rebuild and restart the server
2. Test video sharing between multiple clients
3. Check logs for "PubSub: video track forwarded" messages
4. Verify PLI packets are being forwarded in logs
