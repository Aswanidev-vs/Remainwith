# Video Call Bug Fixes - COMPLETED

## Bug 1: Duplicate transceiver creation (sfu.go) ✅ FIXED
- [x] Remove duplicate transceiver creation in ServeHTTP (lines 163-185)
- [x] Delete AddTransceiverFromKind calls for audio and video

## Bug 2: Client adds tracks after setRemoteDescription (videocall.tmpl) ✅ FIXED
- [x] Add tracks BEFORE connecting in createPeerConnection()
- [x] Remove addTrack calls from handleOffer (lines 1413-1431)
- [x] Remove addTrack calls from startLocalStream (lines 1159-1201)

## Bug 3: Concurrent WebSocket writes (sfu.go) ✅ FIXED
- [x] Add writeCh chan interface{} to Client struct
- [x] Create write goroutine in ServeHTTP
- [x] Replace all client.Conn.WriteJSON with sends to writeCh
- [x] Replace all client.Conn.WriteMessage with sends to writeCh

## Bug 4: Transport closes on disconnected (webrtc_transport.go) ✅ FIXED
- [x] Modify OnConnectionStateChange to only close on failed/closed, not disconnected
- [x] Remove disconnected state from doneCh closure condition

## Additional Fixes ✅ FIXED
- [x] Fix handleConnectionFailure double-lock at lines 330-336
- [x] **CRITICAL**: Client now sends offer first (not SFU) - ensures proper transceiver alignment

## Summary of Changes


### 1. internal/sfu/sfu.go
- Removed duplicate transceiver creation (Bug 1)
- Added `writeCh chan interface{}` to Client struct for WebSocket write serialization (Bug 3)
- Added `writePump()` goroutine to serialize all WebSocket writes (Bug 3)
- Replaced all direct `client.Conn.WriteJSON()` and `client.Conn.WriteMessage()` calls with channel sends (Bug 3)
- Fixed handleConnectionFailure to avoid double-lock issues

### 2. internal/transport/webrtc_transport.go
- Modified `OnConnectionStateChange` to only close `doneCh` on `failed` or `closed` states, not on `disconnected` (Bug 4)
- This allows transient disconnections to recover naturally

### 3. frontend/videocall.tmpl
- Modified `createPeerConnection()` to add local tracks BEFORE any offer/answer exchange (Bug 2)
- Removed track addition from `handleOffer()` (was adding tracks after setRemoteDescription) (Bug 2)
- Removed track addition from `startLocalStream()` (now handled in createPeerConnection) (Bug 2)
- **CRITICAL**: Client now sends offer first via `createAndSendOffer()` function
  - SFU no longer sends initial offer; waits for client offer instead
  - This ensures client's tracks are properly negotiated from the start
  - Prevents transceiver count mismatch between client and SFU


## Verification Steps
1. Build and start the server: `go run main.go`
2. Open two browser tabs and navigate to the video call page
3. Tab 1: Click "Create Room" → note the room code → click "Enter Room"
4. Tab 2: Enter the room code → click "Join Room"
5. Verify bidirectional video: Both tabs should show each other's camera feed
6. Verify bidirectional audio: Both tabs should hear each other's microphone
7. Check browser console for errors (should see successful negotiation)
8. Test reconnection: Briefly disconnect Wi-Fi on one tab and reconnect — verify media resumes

## Expected Console Output (Successful Connection)
```
=== CREATING OFFER FOR SFU ===
Current transceivers: 2
Transceiver[0]: kind=audio, direction=sendrecv
Transceiver[1]: kind=video, direction=sendrecv
Created offer, length: ~3500
Offer has audio: true video: true
Set local description
Final offer length: ~4000
Sent offer to SFU

=== HANDLING OFFER FROM SFU ===
Current transceivers before offer: 2
Current signaling state: stable
Setting remote description...
✓ Remote description set
Transceivers after setting remote description: 3
Created answer
Answer directions - sendrecv: true recvonly: true
Sent answer to SFU
```
