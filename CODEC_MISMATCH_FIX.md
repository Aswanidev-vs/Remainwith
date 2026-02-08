# Codec Mismatch Issue - Root Cause & Fix

## The Problem

From the server logs, we saw:
```
SFU: Error handling answer from client 40: set remote description: unable to start track, codec is not supported by remote
```

And from the browser side:
```
PeerManager: WARNING - No tracks received from client 40 within 15 seconds
```

## Root Cause Analysis

### Issue 1: Overly Strict Codec Parameters
**File:** `internal/transport/webrtc_transport.go` (lines 56-61)

The server was registering Opus with extremely specific parameters:
```go
SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0;maxaveragebitrate=32000;cbr=1"
```

**Why this failed:**
- Servers and browsers negotiate codec parameters differently
- When the server offers with `stereo=0` (mono), it sets a strict expectation
- When the browser's answer tries to use different parameters (e.g., stereo=1 for JS), the negotiation fails
- The error "codec is not supported by remote" means the answer's codec parameters don't match the offer's constraints

### Issue 2: Browser's getUserMedia Success vs RTP Packet Flow
Even if `getUserMedia()` succeeds and tracks are added to the peer connection:
1. The initial offer/answer exchange must succeed for the peer connection to reach "connected" state
2. If the answer fails due to codec mismatch, the peer connection never properly establishes the media path
3. Tracks exist in the peer connection, but RTP ports never negotiate properly
4. Result: Tracks are "added" but never actually send packets

## Fixes Implemented

### Fix #1: Flexible Codec Parameters  
**File:** `internal/transport/webrtc_transport.go` (lines 56-70)

Changed from strict mono + specific bitrate to flexible stereo:
```go
// BEFORE: Impossible to match exactly
SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0;maxaveragebitrate=32000;cbr=1"

// AFTER: Let browser choose flexibility within reason
SDPFmtpLine: "useinbandfec=1"  // Only require FEC for reliability
Channels:     2                  // Match browser defaults (stereo)
```

**Why this works:**
- Browsers default to stereo audio encoding
- Removing "stereo=0" and "cbr=1" lets browsers use their natural settings
- "useinbandfec=1" (Forward Error Correction) is still enabled
- Both sides can now agree on compatible codec parameters

### Fix #2: Better Error Diagnostics
**File:** `internal/transport/webrtc_transport.go` (lines 382-408)

Added helper function and detailed logging:
```go
// NEW: extractCodecsFromSDP() - shows what codecs are being used
// Added to handleAnswer():
if codecs := extractCodecsFromSDP(msg.SDP); len(codecs) > 0 {
    log.Printf("WebRTCTransport: Client %s answer contains codecs: %v", t.clientID, codecs)
}

// Added error context:
log.Printf("WebRTCTransport: CODEC MISMATCH ERROR for client %s: %v", t.clientID, err)
log.Printf("WebRTCTransport: SDP answer (first 1000 chars): %s", msg.SDP[:min(1000, len(msg.SDP))])
log.Printf("WebRTCTransport: Current signalingState: %v", t.peerConn.SignalingState())
```

### Fix #3: Browser getUserMedia Error Handling
**File:** `frontend/videocall.tmpl` (lines 1219-1251)

Added detailed error classification:
```javascript
// NEW: Provides specific error messages
if (err.name === 'NotAllowedError') {
    errorMsg = 'Camera/Microphone permission denied.';
} else if (err.name === 'NotFoundError') {
    errorMsg = 'No camera or microphone found.';
} else if (err.name === 'NotReadableError') {
    errorMsg = 'Camera/Microphone is in use.';
} // ... etc
```

## How to Test

### Before Testing, Clear Cache:
```bash
# Browser: Cmd/Ctrl + Shift + Delete, clear all caches
# Or open Developer Tools > Application > Storage > Clear All
```

### Test Steps:
1. **Recompile:**
   ```bash
   go build && ./main
   ```

2. **Browser Test:**
   - Open browser and go to localhost:8080
   - Should see permission prompt for Camera/Microphone
   - DENY permission first to test error message
   - Then ALLOW permission
   - Join a room

3. **Check Logs for:** 
   ```
   WebRTCTransport: Client 22 answer contains codecs: [opus/48000/2 vp8/90000]
   ✓ (NOT: multiple registrations or mismatches)
   ```

4. **Expected Success Behavior:**
   ```
   SFU: Received answer from client 22 (len=2392)
   SFU: Answer processed successfully for client 22
   ✓ (NOT: Error handling answer)
   ```

5. **Browser Console Should Show:**
   ```
   ✓ Local stream started with 2 tracks
   ✓ Video packets: XXXX bytes
   ✓ REMOTE TRACK RECEIVED
   ```

## Why Audio/Video Still Might Not Work

Even with this fix, media might not flow if:

1. **Firewall/NAT Issues** - TURN relay not connecting
   - Check: Browser console for ICE candidate types
   - Should see: `relay` candidates (TURN)

2. **Permission Issues** - User denies camera/mic
   - Browser will show specific error message now
   - Accept permission prompt

3. **Browser Codec Support** - Browser doesn't support VP8/Opus
   - Less common on modern browsers
   - Check: Browser console for codec support info

4. **WebRTC Sandboxing** - Browser isolates localhost from camera
   - VS Code tunnels may have restrictions
   - Solution: Use public HTTPS server

5. **SFU Track Reception** - Tracks still not reaching server
   - Check log: "WARNING - No tracks received from client"
   - This means browser isn't sending tracks to SFU
   - Likely a peer connection state issue, not codec related

## Monitoring Logs After Fix

### Success Pattern:
```
Client connects:
  ✓ Added sendrecv transceivers
  ✓ Received answer from client
  ✓ Answer processed successfully
  
Tracks published:
  ✓ Received remote track (from other client)
  ✓ Starting track forwarding
  ✓ Auto-subscribed client B to track from A
```

### Failure Pattern to Watch For:
```
✗ Error handling answer from client: set remote description: ...
  → Codec mismatch (should be fixed now)
  
✗ WARNING - No tracks received from client within 15 seconds
  → Browser not sending tracks (CRITICAL - investigate why)
  → Maybe getUserMedia failed?
  → Maybe tracks not added to PC?
  
✗ PubSub: WARNING - No subscribers for track
  → Subscription timing issue
```

## Next Steps if Audio/Video Still Doesn't Work

1. Check browser console for any JavaScript errors
2. Check if getUserMedia() permission dialog appears and user accepts
3. Look for "WARNING" messages in server logs
4. Verify TURN servers are actually being used (check ICE candidates)
5. Test with different browser (Chrome, Firefox, Safari)
6. Check network for blocked ports (3478, 443, 80 for TURN)
