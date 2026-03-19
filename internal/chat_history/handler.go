package chat_history

import (
	"Remainwith/db"
	"Remainwith/internal/handler"
	"encoding/json"
	"net/http"
	"strconv"
)

func ChatHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	receiverID := r.URL.Query().Get("receiver")
	groupIDStr := r.URL.Query().Get("group")
	limitStr := r.URL.Query().Get("limit")
	beforeStr := r.URL.Query().Get("before")

	var groupID *int
	if groupIDStr != "" {
		gid, _ := strconv.Atoi(groupIDStr)
		groupID = &gid
	}

	limit := 30
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}

	var beforeID *int64
	if beforeStr != "" {
		bid, _ := strconv.ParseInt(beforeStr, 10, 64)
		beforeID = &bid
	}

	msgs, err := db.GetMessages(r.Context(), userID, receiverID, groupID, limit, beforeID)

	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Reverse for chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func DeleteMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MessageID int64 `json:"messageID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.MessageID == 0 {
		http.Error(w, "messageID is required", http.StatusBadRequest)
		return
	}

	if err := db.DeleteMessage(r.Context(), req.MessageID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
