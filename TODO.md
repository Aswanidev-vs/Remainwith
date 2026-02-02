# Group Chat Implementation TODO

- [x] Add `IsGroup bool` field to Message struct in `internal/models/model.go`
- [x] Update hub.go to set `IsGroup = true` when ReceiverID == "all"
- [x] Add `<option value="all">Group Chat</option>` to receiver select in `frontend/chat.tmpl`
- [x] Update JavaScript in chat.tmpl to set `IsGroup` when sending to "all"
- [x] Modify message display logic to show sender names for group messages and indicate group vs private
- [ ] Test group chat functionality with multiple users
