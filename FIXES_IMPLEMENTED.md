# Audio & Video Packet Issue - FIXES IMPLEMENTED

## Summary of Changes

### 1. **Fixed Transceiver Direction (CRITICAL FIX)**  
**File:** [internal/sfu/sfu.go](internal/sfu/sfu.go#L195-L227)

**Change:** Changed from `RTPTransceiverDirectionRecvonly` to `RTPTransceiverDirectionSendrecv`

**Why:** 
- Clients now have transceivers that can both SEND (to SFU) and RECEIVE (from SFU relaying other clients' media)
- **recvonly** transceivers cannot RECEIVE media from new senders - they only receive the initial offer's media
- **sendrecv** transceivers allow the SFU to add and send new tracks without renegotiation for each track

**Code Change:**
```go
// BEFORE:
Direction: webrtc.RTPTransceiverDirectionRecvonly

// AFTER:
Direction: webrtc.RTPTransceiverDirectionSendrecv
```

### 2. **Removed Duplicate Transceiver Addition (HIGH FIX)**  
**File:** [internal/sfu/sfu.go](internal/sfu/sfu.go#L447-L461)

**Change:** Removed the `client.Negotiator.AddTransceiverFromKind()` call after `AddTrack()`

**Why:**
- `AddTrack()` automatically uses an existing sendrecv transceiver
- Calling `AddTransceiverFromKind()` afterwards was creating DUPLICATE transceivers
- This caused mismatched offer/answer negotiation
- Now we just trigger `Negotiate()` if needed instead of queuing a transceiver request

**Code Change:**
```go
// BEFORE:
client.Negotiator.AddTransceiverFromKind(TransceiverRequest{
    CodecType: trackToForward.Kind(),
    Init: webrtc.RTPTransceiverInit{
        Direction: webrtc.RTPTransceiverDirectionSendonly, // ← WRONG: trying to add new transceiver
    },
})

// AFTER:
if pc != nil {
    signalingState := pc.SignalingState()
    if signalingState == webrtc.SignalingStateStable {
        log.Printf("SFU: Triggering renegotiation for client %s to add track %s", client.ID, pubTrack.TrackID)
        client.Negotiator.Negotiate()
    }
}
```

### 3. **Added pub_track Message Handler in Browser (CRITICAL FIX)**  
**File:** [frontend/videocall.tmpl](frontend/videocall.tmpl#L1376-1384)

**Change:** Added handling for `pub_track` and `add` message types in WebSocket handler

**Why:**
- The SFU sends `pub_track` events to notify clients about new published tracks
- Browser was **silently ignoring** these messages
- Now properly handles them (though actual track reception is via WebRTC ontrack handler)

**Code Change:**
```javascript
// ADDED:
case 'add':
case 'pub_track':
    // Pub track message - SFU is notifying us that a remote peer published a track
    console.log('Pub track notification received:', msg.kind, 'from', msg.pub_client_id);
    // Don't need to do anything - remote tracks will be received via ontrack handler
    break;
```

### 4. **Enhanced Browser Diagnostics**  
**File:** [frontend/videocall.tmpl](frontend/videocall.tmpl#L753-819)

**Changes:**
- Added detailed logging of transceiver information
- Added packet monitoring for both audio and video remote tracks
- Logs when RTP packets are actually received from remote peers
- Helps diagnose if packets are arriving or being dropped

**Key Diagnostic Logs:**
```
✓ VIDEO PACKETS RECEIVED: XXXX bytes, YY frames decoded
✓ AUDIO PACKETS RECEIVED: XXXX bytes  
⚠️ Video track received but NO PACKETS! (bytesReceived=0)
```

### 5. **Enhanced Server Diagnostics**  
**File:** [internal/sfu/pubsub/pubsub.go](internal/sfu/pubsub/pubsub.go#L425-430)

**Changes:**
- Added logging when no subscribers exist for a track (packets would be dropped)
- Added logging for failed forward attempts
- Helps identify subscription timing issues

**Key Diagnostic Logs:**
```
PubSub: WARNING - No subscribers for client XXXX! Packets will be dropped.
PubSub: WARNING - No subscribers for track XXXX from YYYY
PubSub: WARNING - Failed to forward packet to any subscribers for track XXXX
```

## How to Test the Fixes

### Test Steps:
1. **Compile & Run:**
   ```bash
   go build
   ./main
   # OR use: air (with .air.toml already set up)
   ```

2. **Open Two Browser Windows:**
   - Window A: http://localhost:8080 (or your server address)
   - Window B: http://localhost:8080 (same server, second user)

3. **Create or Join Room:**
   - User A: Click "Create Room"
   - Share the Room ID with User B  
   - User B: Enter Room ID and "Join Room"

4. **Check Browser Console (F12):**
   - Should see: `✓ VIDEO PACKETS RECEIVED` and `✓ AUDIO PACKETS RECEIVED`
   - If NOT seeing packets: Check the WARNING messages

5. **Check Server Logs:**
   - Look for `sendrecv` in transceiver logs (should NOT be `recvonly` anymore)
   - Look for `Auto-subscribed` logs
   - Look for `Successfully subscribed` logs in SFU
   - Look for `Starting track forwarding` logs in pubsub
   - Look for any `WARNING` logs about missing subscribers

## Remaining Issues to Monitor

### 1. **Race Condition in Subscription Timing**  
**File:** [internal/sfu/sfu.go](internal/sfu/sfu.go#L388-425)

**Status:** PARTIALLY FIXED

The subscription now happens immediately via `pubsub.Sub()`, and `forwardTrack()` starts immediately. But:
- Browser might not have processed the new offer/answer yet
- Packets might arrive before the transceiver is ready

**Mitigation:** Using pre-allocated sendrecv transceivers should minimize this issue, but monitor logs for:
```
PubSub: WARNING - No subscribers for track
```

If this appears frequently, packet loss is happening from race conditions.

### 2. **Renegotiation Timing**  
**File:** [internal/sfu/sfu.go](internal/sfu/sfu.go#L447-461)

**Status:** IMPROVED but needs monitoring

If signaling state is not stable, renegotiation might queue without being processed. Monitor logs for:
```
SFU: Queued renegotiation
SFU: Execute queued negotiation  
```

## Debugging Commands

### Check Server Logs for Issues:
```bash
# All SFU logs
grep "SFU:" go.log | tail -50

# All PubSub logs  
grep "PubSub:" go.log | tail -50

# Warning logs (packet loss indicators)
grep "WARNING" go.log

# Track forwarding logs
grep "Starting track forwarding" go.log
grep "successfully subscribed" go.log
```

### Check Browser Network Tab:
1. Open DevTools (F12)
2. Go to "Network" tab
3. Filter to WebSocket connections
4. Check messages sent/received between browser and SFU
5. Look for "offer" and "answer" messages

### Check Browser Console:
1. Open DevTools (F12)
2. Go to "Console" tab
3. Filter by "VIDEO PACKETS" or "AUDIO PACKETS"
4. Should see packets flowing after ~2-3 seconds

## If Issues Persist

### Checklist:
- [ ] Server logs show `sendrecv` transceivers (not `recvonly`)
- [ ] Browser shows packets being sent from local camera (check "Video packets" monitor)
- [ ] Server shows `Starting track forwarding` for each published track
- [ ] Server shows `Auto-subscribed` for other clients
- [ ] Browser shows `REMOTE TRACK RECEIVED` for video and audio
- [ ] Browser shows `✓ VIDEO PACKETS RECEIVED` and `✓ AUDIO PACKETS RECEIVED`

### If Browser Not Receiving Packets:
1. Check browser console for `⚠️ Video track received but NO PACKETS!`
2. Check server logs for `No subscribers for track`
3. Check WebSocket messages in Network tab to ensure offer/answer exchanged
4. Check transceiver states in browser console

### If Server Not Forwarding Packets:
1. Check server logs for `WARNING` messages
2. Verify subscriptions are created: grep for `succeeded to auto-subscribe`
3. Check if RTP forwarding started: grep for `Starting track forwarding`
4. Monitor output of pubsub forwarding: grep for error messages

## Next Steps

1. **Run tests with these fixes in place**
2. **Clear browser cache and retry** (in case old code cached)
3. **Monitor logs for WARNING messages** (indicates remaining issues)
4. **If still not working:**
   - Enable more verbose logging
   - Implement WebRTC stats monitoring
   - Check firewall/NAT settings
   - Test with TURN relay specifically
