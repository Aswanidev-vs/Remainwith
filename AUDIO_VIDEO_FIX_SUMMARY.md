# Audio and Video Packet Sharing Fix

## Problem Identified
Audio and video packets were not being properly shared between clients because of race conditions and synchronization issues in the publish/subscribe packet forwarding system.

## Root Causes
1. **Race Condition in forwardTrack()**: The function was accessing `ps.publishedTracks[clientID][trackID]` to determine the track kind WITHOUT holding a synchronization lock. This caused issues when multiple tracks (audio and video) were published simultaneously.

2. **Improper Lock Release Timing**: The `Pub()` function was spawning the forwarding goroutine while still holding the lock, then releasing it. This could lead to race conditions when the goroutine tried to access the track information.

3. **Missing Defensive Checks**: The forwarding functions weren't properly initializing subscription maps when they didn't exist yet, potentially causing nil reference issues.

## Fixes Applied

### 1. Fix Race Condition in Track Kind Detection (Internal/sfu/pubsub/pubsub.go)
**Changed:**
- Pass `trackKind` directly as a parameter to `forwardTrack()` instead of looking it up in the map within the goroutine
- Determine track kind BEFORE spawning the forwarding goroutine
- This eliminates the race condition where audio/video determination could fail

**Before:**
```go
func (ps *PubSub) forwardTrack(clientID, trackID string, reader *TrackReader) {
    isAudioTrack := false
    if track, ok := ps.publishedTracks[clientID][trackID]; ok {
        isAudioTrack = track.Kind == "audio"
    }
    // ... rest of function
}
```

**After:**
```go
func (ps *PubSub) forwardTrack(clientID, trackID string, reader *TrackReader, trackKind string) {
    isAudioTrack := trackKind == "audio"
    // ... rest of function
}
```

### 2. Improved Lock Management in Pub Function
**Changed:**
- Keep the lock while creating and notifying about the track
- Release the lock BEFORE spawning the forwarding goroutine
- This ensures all track information is properly published before forwarding begins

**Key Change:**
```go
func (ps *PubSub) Pub(clientID string, reader *TrackReader) error {
    ps.mu.Lock()
    // ... add track to maps ...
    ps.notifyEventSubscribers(PubTrackEvent{...}) // Within lock
    ps.mu.Unlock() // Unlock BEFORE spawning goroutine
    
    // Start forwarding with track kind already determined
    go ps.forwardTrack(clientID, trackID, reader, trackKind)
    return nil
}
```

### 3. Add Defensive Subscription Map Initialization
**Changed:**
- Added explicit check and initialization of subscription maps when they don't exist
- Prevents potential nil reference issues in forwarding loops
- Better error handling when subscribers haven't joined yet

**Added Check:**
```go
if !ok {
    // Initialize the subscriptions map for this clientID if it doesn't exist
    ps.mu.Lock()
    if ps.subscriptions[clientID] == nil {
        ps.subscriptions[clientID] = make(map[string]map[string]*Sub)
    }
    ps.mu.Unlock()
    // Continue buffering packets...
    continue
}
```

### 4. Parameter Naming Clarity
**Changed:**
- Renamed parameter in `forwardDirect()` from `isAudioTrack` to `isVideoTrack`
- Makes the logic clearer: `isVideoTrack=true` means it's a video track, `isVideoTrack=false` means it's audio
- Updated all related logging and conditional logic accordingly

## Testing Recommendations
1. **Create a room with two clients**
   - Client A: Enables camera + microphone (publishes audio and video tracks)
   - Client B: Joins the room and subscribes

2. **Verify both streams are shared:**
   - ✓ Audio can be heard from Client A
   - ✓ Video can be seen from Client A
   - Check logs for proper track publication and subscription messages

3. **Monitor packet forwarding:**
   - Look for "PUBLISHED" messages for both audio and video tracks
   - Verify "SUBSCRIBED" messages show both tracks being subscribed by Client B
   - Check forwarding stats in logs show packets being transferred for both audio and video

## Expected Log Output
```
PubSub: [TRACK <id>] PUBLISHED - clientID: <A>, kind: audio
PubSub: [TRACK <id>] Starting direct forwarding for audio track from client <A>
PubSub: [TRACK <id>] PUBLISHED - clientID: <A>, kind: video
PubSub: [TRACK <id>] Starting direct forwarding for video track from client <A>
PubSub: [TRACK <id>] SUBSCRIBED - pubClientID: <A>, subClientID: <B>, kind: audio
PubSub: [TRACK <id>] SUBSCRIBED - pubClientID: <A>, subClientID: <B>, kind: video
PubSub: [TRACK <id>] VIDEO STATS: forwarded N total packets to 1/1 subscribers
```

## Changes Made
- Modified: `internal/sfu/pubsub/pubsub.go`
  - Fixed `Pub()` function to determine and pass track kind
  - Updated `forwardTrack()` signature to accept track kind parameter
  - Enhanced `forwardDirect()` with defensive initialization
  - Improved parameter naming in forwarding functions
  - Better synchronization of lock release timing

## Impact
- **Fixes**: Audio and video now properly share the same packet forwarding infrastructure
- **Performance**: Eliminates race conditions that could cause packet loss
- **Reliability**: Better handling of edge cases where subscribers join/leave during packet forwarding
