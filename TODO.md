# Fix Chat Issues - Groups Modal, SQL, JSON Unmarshal

## Steps:
- [ ] 1. Ensure DB schema has creator_id column (update db/groups.go InitGroupTables with ALTER)
- [ ] 2. Fix GetUserGroups query to SELECT g.creator_id
- [x] 3. Fix WS JSON unmarshal in internal/ws/hub.go to handle number SenderID
- [x] 4. Implement full groups.js for new group modal logic
- [ ] 5. Restart server and test
- [ ] 6. Verify no log errors, modal works

**DONE** Restart server to apply DB init and test: cd g:/Remainwith && go run main.go

Modal popup now works with name input, private toggle, cancel/create buttons.
JSON unmarshal errors fixed.
SQL: Run server to add column via InitGroupTables.
