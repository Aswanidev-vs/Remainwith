# Port Forwarding WebRTC Fix

## Problem
When using port forwarding (VS Code tunnels, ngrok, etc.), remote video was not displaying even though both users connected successfully. The logs showed:
- "Processing 0 queued track events" - no media tracks being received
- Connection successful but no media flowing through
- Root cause: **Media tracks not being delivered through the forwarded connection**

## Root Causes
1. **Direct P2P connections fail through port forwarding** - Direct UDP connections between peers don't work
2. **STUN servers prioritized over TURN** - The WebRTC config had STUN first, causing attempts at direct connection
3. **Insufficient ICE candidate filtering** - Host candidates were being used instead of relay
4. **Poor diagnostics** - No logging to show why tracks weren't being received

## Fixes Applied

### 1. Frontend: ICE Server Configuration ✅
**File**: `frontend/videocall.tmpl`

**Change**: Reordered ICE servers to prioritize TURN relay
```javascript
iceServers: [
    // TURN servers FIRST - these work through port forwarding
    { urls: ['turn:openrelay.metered.ca:80', 'turn:openrelay.metered.ca:443', ...], ... },
    // STUN servers as fallback only
    { urls: 'stun:stun.l.google.com:19302' },
    ...
]
```

**Why**: TURN servers provide relay functionality that works through firewalls and port forwarding. By placing them first, WebRTC will prefer relay candidates.

### 2. Frontend: Enhanced Track Diagnostics ✅
**File**: `frontend/videocall.tmpl`

**Changes**:
- Added comprehensive logging when local tracks are added
- Monitor sender statistics to detect if tracks are actually being sent
- Check active ICE candidate pairs to see if relay is being used
- Show warnings if sending 0 bytes (indicates tunnel issue)
- Detect port forwarding and log appropriate messages

```javascript
// Monitor sender stats
pc.getStats(sender).then(stats => {
    stats.forEach(report => {
        if (report.type === 'outbound-rtp') {
            if (report.bytesSent === 0 && report.packetsSent === 0) {
                console.warn('⚠️  Track not sending data! Check firewall/network.');
                if (isPortForwarded) {
                    console.warn('ℹ️  Using port forwarding - ensure TURN relay is used.');
                }
            }
        }
    });
});
```

### 3. Frontend: Better Local Stream Logging ✅
**File**: `frontend/videocall.tmpl`

**Change**: Added detailed track information logging
```javascript
localStream.getTracks().forEach(track => {
    console.log('Local track:', {
        kind: track.kind,
        id: track.id,
        enabled: track.enabled,
        readyState: track.readyState,
        settings: track.getSettings()
    });
});
```

### 4. Backend: Track Reception Timeout Warning ✅
**File**: `internal/sfu/peer_manager.go`

**Change**: Added 15-second timeout that logs warning if no tracks received
```go
trackReceiveTimeout := time.NewTimer(15 * time.Second)

case <-trackReceiveTimeout.C:
    if !trackReceived {
        log.Printf("PeerManager: WARNING - No tracks received from client %s within 15 seconds. " +
            "Check if browser is sending media tracks. Media may not work.", clientID)
    }
```

**Why**: This helps diagnose the issue - if no tracks arrive 15 seconds after connection, the problem is at the browser level, not the backend.

### 5. Frontend: Port Forwarding Detection ✅
**File**: `frontend/videocall.tmpl`

**Change**: Automatic detection of port forwarding scenarios
```javascript
const isPortForwarded = window.location.protocol === 'http:' || 
                      window.location.hostname === 'localhost' || 
                      window.location.hostname === '127.0.0.1' ||
                      window.location.hostname.includes('.app.github.dev');
```

**Why**: Allows conditional optimization for port forwarding connections.

## How to Verify the Fix

### Browser Console (F12 → Console)
Look for these positive indicators:

✅ **Good signs:**
```
✓ Added local track: {kind: "audio", id: "...", enabled: true}
✓ Added local track: {kind: "video", id: "...", enabled: true}
✓ Using TURN relay candidate (good for port forwarding)
✓ audio track is sending data (bytesSent > 0)
✓ video track is sending data (bytesSent > 0)
=== REMOTE TRACK RECEIVED ===
```

❌ **Bad signs:**
```
⚠️ Track not sending data! Check firewall/network.
⚠️ No TURN relay candidate found
Processing 0 queued track events
```

### Server Logs
Look for:
```
✅ Good: PeerManager: Received remote track [trackID] (audio|video) from client [ID]
❌ Bad: WARNING - No tracks received from client 40 within 15 seconds
```

## What Happens Now

1. **User 1 joins room** → Creates peer connection with TURN relay priority
2. **User 1 sends local tracks** → Uses TURN server for media relay
3. **User 2 joins room** → Also uses TURN server
4. **SFU receives both tracks** → Timestamps show track reception in logs
5. **SFU forwards tracks** → Each user receives other's video/audio
6. **Remote video displays** → Video element appears in browser

## Troubleshooting If Still Not Working

### Check the Browser Console:
```javascript
// Run this in console to see connection details
pc.getStats().then(stats => {
    stats.forEach(report => {
        if (report.type === 'candidate-pair' && report.state === 'succeeded') {
            console.log('Active ICE pair:', report);
        }
    });
});
```

### Check Server Logs:
```bash
# Look for track reception
grep "Received remote track" logs.txt

# Check for warnings
grep "WARNING" logs.txt

# Check timing
grep "Initial connection established" logs.txt
```

### Common Issues:

**Issue:** "bytesSent: 0, packetsSent: 0" in console
- The connection is established but media isn't flowing
- **Solution**: Ensure TURN server is accessible, check firewall rules

**Issue:** "Processing 0 queued track events" in server logs
- First user's tracks never reached the SFU
- **Solution**: Check browser console for errors, verify media permissions

**Issue:** "ICE connection state stuck on 'checking'"
- ICE candidates not working properly
- **Solution**: Verify TURN server credentials are correct

## Configuration for Production

For production deployments with port forwarding, consider:

1. **Use a dedicated TURN server** instead of public free ones:
   ```javascript
   {
       urls: 'turn:your-turn-server.com:3478',
       username: 'your-username',
       credential: 'your-password'
   }
   ```

2. **Add multiple TURN servers** for redundancy:
   ```javascript
   iceServers: [
       { /* primary TURN */ },
       { /* secondary TURN */ },
       { /* backup TURN */ },
       { /* stun fallback */ }
   ]
   ```

3. **Monitor TURN server health** regularly

## Files Modified

1. ✅ `frontend/videocall.tmpl` - ICE config, diagnostics, logging
2. ✅ `internal/sfu/peer_manager.go` - Track timeout warning
