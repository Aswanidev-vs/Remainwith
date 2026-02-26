# WebRTC SFU 10 Phases Implementation

## Progress Tracker

### Phase 1: Jitter Buffer & NACK Recovery
- [ ] Create jitter buffer implementation
- [ ] Implement NACK handler
- [ ] Integrate with pubsub

### Phase 2: Bandwidth Optimization (Simulcast + Congestion Control)
- [ ] Create simulcast layer selector
- [ ] Implement congestion controller
- [ ] Add GCC algorithm

### Phase 3: Video Packet Fix (SSRC preservation)
- [ ] Remove SSRC rewriting in pubsub
- [ ] Preserve original SSRC values

### Phase 4: Session Management (Immediate subscription, cleanup)
- [ ] Enhance tracks manager
- [ ] Add immediate subscription
- [ ] Improve cleanup

### Phase 5: Audio Noise Fix (Proper Opus settings)
- [ ] Update WebRTC transport with Opus settings
- [ ] Add noise suppression

### Phase 6: Video Packet Forwarding Fix (No cloning, peer-calls pattern)
- [ ] Refactor packet forwarding
- [ ] Remove packet cloning

### Phase 7: Video Sharing & Audio Noise Fix (Auto-subscription + noise gate)
- [ ] Implement auto-subscription
- [ ] Add noise gate

### Phase 8: Video Freezing Fix (Remove duplicate RTP reader)
- [ ] Audit and remove duplicate readers
- [ ] Fix reader lifecycle

### Phase 9: Connection Stability Fix (ICE keepalive settings)
- [ ] Add ICE keepalive
- [ ] Enhance connection stability

### Phase 10: ICE Restart Support (Handle connection failures)
- [ ] Implement ICE restart
- [ ] Add reconnection logic
