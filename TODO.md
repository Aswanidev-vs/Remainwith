# Video Calling System Fix - TODO

## Completed Tasks
- [x] Updated SFU to ignore unknown message types (mute/camera_toggle) instead of causing unmarshaling errors
- [x] Updated frontend to connect to two separate WebSockets:
  - `/ws/sfu` for WebRTC signaling (offer/answer/candidate)
  - `/ws/signaling` for room management (mute/camera_toggle/join)
- [x] Modified frontend message routing:
  - WebRTC messages (offer/answer/candidate) sent to SFU WebSocket
  - Room management messages (mute/camera_toggle) sent to signaling WebSocket
- [x] Updated endCall to close both WebSockets properly
- [x] Added handleOffer function for WebRTC offer handling

## Pending Tasks
- [ ] Test that video calls work and users can see each other
- [ ] Verify that mute/camera toggle messages are handled correctly via the signaling WebSocket
- [ ] Ensure no more JSON unmarshaling errors occur

## Next Steps
1. Test the video calling functionality with multiple users
2. Monitor server logs for any remaining errors
3. Verify that room management features work as expected
