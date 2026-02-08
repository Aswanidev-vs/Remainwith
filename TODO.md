# WebRTC/SFU Fix TODO

## Tasks
- [x] 1. Fix Frontend Transceiver Handling (frontend/videocall.tmpl)
  - Removed early transceiver creation
  - Changed to add tracks directly using addTrack() before setRemoteDescription
  - Fixed answer creation flow to ensure sendrecv direction
  
- [x] 2. Fix Backend Codec Registration (internal/transport/webrtc_transport.go)
  - Added proper VP8/Opus codec parameters with standard payload types
  - Added RTX codec (payload type 97) for reliability
  - Added VP9 (payload type 98) and H264 (payload type 102) for broader compatibility
  - Removed upfront transceiver creation - now created on track add
  
- [x] 3. Fix SFU Answer Handling (internal/sfu/sfu.go)
  - Improved ICE candidate queuing (already implemented)
  - Added answer SDP direction logging (sendrecv/sendonly/recvonly)
  - Fixed transceiver direction handling with better logging
  - Fixed compiler errors (undefined pc, CurrentDirection)

- [ ] 4. Test and verify fixes
  - Rebuild and restart the server
  - Test video call between two participants
  - Verify media flows in both directions
  - Check browser console for "sendrecv" in answer SDP
