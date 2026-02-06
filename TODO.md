# Video Call Fix TODO

## Issues to Fix:
- [ ] Frontend: Fix remote video display logic for multiple participants
- [ ] Frontend: Improve stream/track ID handling 
- [ ] Frontend: Fix CSS layout for multiple remote videos
- [ ] Backend: Fix SSRC rewriting in RTP packet forwarding
- [ ] Backend: Ensure proper track subscription timing
- [ ] Test: Verify users can see each other

## Progress:
- Analyzed frontend videocall.tmpl - found issues with video element handling
- Analyzed backend pubsub.go - found SSRC rewriting issue
- Analyzed backend sfu.go - found track subscription timing issue
