# WebRTC SFU Fix Plan

## Issues Identified

1. **Client not sending media tracks**: The client's answer SDP has `recvonly` direction instead of `sendrecv`, so the client isn't actually sending media to the SFU.

2. **ICE timeout**: ICE gathering timeout is set to 5 seconds which may be too short, and the client might not be properly handling ICE candidates.

3. **Transceiver direction mismatch**: SFU creates `recvonly` transceivers but client doesn't properly respond with `sendrecv` in the answer.

## Fix Steps

### 1. Fix Client Transceiver Direction (frontend/videocall.tmpl)
- [ ] Modify `handleOffer` function to set transceiver direction to `sendrecv` before creating answer
- [ ] Ensure local tracks are properly matched to transceivers
- [ ] Add better logging for transceiver states

### 2. Fix ICE Handling (frontend/videocall.tmpl)
- [ ] Improve ICE candidate handling in `handleOffer`
- [ ] Ensure ICE candidates are sent after answer is created
- [ ] Add ICE gathering state monitoring

### 3. Verify SFU Answer Handling (internal/sfu/sfu.go)
- [ ] Add logging to verify transceiver directions after answer is received
- [ ] Ensure SFU properly processes client answer

### 4. Test and Verify
- [ ] Test video call with room creation
- [ ] Verify tracks are received within 15 seconds
- [ ] Check ICE connection establishes properly
