# Audio/Video Fix - Verification & Troubleshooting Guide

## Quick Verification Checklist

After applying the fix, follow these steps to verify audio/video transmission works:

### Step 1: Build the Application
```bash
cd g:/Remainwith
go build -o remainwith.exe
echo "Setup complete - Audio/video fix applied"
```

### Step 2: Start the Application
```bash
./remainwith.exe
# Should start on http://localhost:8080 or configured port
```

### Step 3: Test with Two Browsers

#### Open Browser 1
1. Go to `http://localhost:8080/video`
2. Click "Create Room" 
3. Copy the 6-digit room ID shown
4. Open browser DevTools (F12) → Console tab
5. Grant camera/microphone permissions

#### Open Browser 2 (Different machine or incognito window)
1. Go to `http://localhost:8080/video`
2. Click "Join Room"
3. Paste the 6-digit room ID
4. Click "Join"
5. Open browser DevTools (F12) → Console tab
6. Grant camera/microphone permissions

### Step 4: Verify Console Output

#### Browser 2 Console (joining browser) should show:
```
✓ Local stream started
✓ SFU WebSocket connected
✓ Sent ready signal to SFU, waiting for offer
✓ Handling offer from SFU
✓ Remote description set
✓ Created answer
✓ Sent answer to SFU
✓ Connection state: connected

[NEW MESSAGES AFTER FIX]
✓ SFU notified: Remote track available {
    pubClientId: "user-xxx",
    trackId: "video-xxx",
    kind: "video"
  }
✓ Waiting for ontrack event to receive actual media...
✓ Remote track received: video
✓ Added video track to subscriber
```

### Step 5: Visual/Audio Verification

#### What You Should See:
- ✓ Remote participant's video in full screen
- ✓ Local participant's video in corner (mirror image)
- ✓ Control buttons working (mute, camera, hang up)
- ✓ Both participants show "Connected - 2 participant(s)"

#### What You Should Hear:
- ✓ Remote participant's audio clearly
- ✓ Your own audio NOT echoing (local is muted for you)

## Troubleshooting If Audio/Video Still Doesn't Work

### Issue #1: No pub_track messages in console
**Symptom:** Console shows "SFU WebSocket connected" but no "SFU notified" messages

**Cause:** SFU not sending tracks or connection established before pub_track listener ready

**Solution:**
1. Check console for errors
2. Verify both users show "Connected - 2 participant(s)"
3. If not, wait 5 seconds - may still be establishing
4. Refresh page and try again

### Issue #2: pub_track messages appear but no ontrack event
**Symptom:** See "SFU notified" messages but no "Remote track received"

**Cause:** ontrack handler not firing (usually transceiver issue on server)

**Solution:**
1. Check server logs for transceiver setup errors
2. Verify SFU is properly adding transceivers
3. Check if RTP packets reaching client using WebRTC stats:
   ```javascript
   // In browser console:
   pc.getStats().then(report => {
       report.forEach(stat => {
           if (stat.type === 'inbound-rtp') {
               console.log('Inbound RTP:', stat.kind, stat.bytesReceived);
           }
       });
   });
   ```

### Issue #3: Video displays but audio is silent
**Symptom:** See video from remote participant but no audio

**Cause:** Audio track added but muted or not properly configured

**Solution:**
1. Check in console: "videoData.audioTrack" should be truthy
2. Verify audio constraints set: they should show `echo_cancellation: true`
3. Check remote video element: `videoEl.muted` should be `false`

### Issue #4: Only one direction works (you see them but they don't see you)
**Symptom:** Asymmetric video/audio flow

**Cause:** Local tracks not properly published or SFU not forwarding

**Solution:**
1. Check local stream: Browser console should show "Added video track" and "Added audio track"
2. Verify stats:
   ```javascript
   // In browser console:
   pc.getSenders().forEach(sender => {
       console.log('Sending:', sender.track?.kind, sender.track?.enabled);
   });
   ```
3. Check server logs for track publication errors

### Issue #5: "Connection failed" or frequent disconnects
**Symptom:** Shows "Connection failed" message or keeps reconnecting

**Cause:** Network issues or SFU server not responding

**Solution:**
1. Check network connectivity - can you access other sites?
2. Check if port forwarding is working:
   ```bash
   # Test from same machine
   curl http://localhost:8080/health
   # If using remote, check firewall rules
   ```
3. Verify SFU server is running and not erroring
4. Check browser console for WebSocket connection errors

## Advanced Debugging

### Enable Detailed Logging
Add this to browser console to see detailed stats:
```javascript
// Monitor connection quality every 2 seconds
setInterval(() => {
    if (!pc) return;
    pc.getStats().then(report => {
        report.forEach(stat => {
            if (stat.type === 'inbound-rtp' && stat.kind === 'video') {
                console.log('📹 Video In:', {
                    bytes: stat.bytesReceived,
                    packets: stat.packetsLost,
                    jitter: stat.jitter
                });
            }
            if (stat.type === 'outbound-rtp' && stat.kind === 'video') {
                console.log('📹 Video Out:', {
                    bytes: stat.bytesSent,
                    frames: stat.framesEncoded,
                    fps: stat.framesEncodedPerSecond
                });
            }
        });
    });
}, 2000);
```

### Server-Side Diagnostics
Check Go application logs for:
```
"SFU: Track event received" - Track being forwarded
"SFU: Error adding track to subscriber" - Track add failures
"SFU: Client X joined room Y" - Connection established
```

## Port Forwarding NOT Required for Local Testing

If testing locally on same machine, you do NOT need port forwarding:
- ✓ `localhost:8080` works fine
- ✓ VSCode debug mode doesn't interfere
- ✓ Only need port forwarding for remote connections (different machines)

## Expected Performance

After successful fix:
- **Video:** Should display within 1-2 seconds of joining
- **Audio:** Should start within 1-2 seconds of joining
- **Frame Rate:** 15-30 FPS depending on network
- **Latency:** 100-500ms typical

## Success Confirmation

✓ **If you see this, the fix works:**
1. Browser console shows "SFU notified: Remote track available"
2. Remote participant's video displays in the video container
3. Remote participant's audio is audible
4. Local camera video shows in corner
5. Status shows "Connected - 2 participant(s)"

The audio/video packet transmission issue is fixed!
