package chat

import (
	"Remainwith/db"
	"Remainwith/internal/handler"
	"encoding/json"
	"net/http"
	"strconv"
)

func CreateGroupHandler(w http.ResponseWriter, r *http.Request) {
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
		Name      string `json:"name"`
		IsPrivate bool   `json:"isPrivate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Group name is required", http.StatusBadRequest)
		return
	}

	g, err := db.CreateGroup(r.Context(), req.Name, req.IsPrivate, userID)
	if err != nil {
		http.Error(w, "Failed to create group", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

func GetMyGroupsHandler(w http.ResponseWriter, r *http.Request) {
	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	groups, err := db.GetUserGroups(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to fetch groups", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

func GetPublicGroupsHandler(w http.ResponseWriter, r *http.Request) {
	groups, err := db.GetPublicGroups(r.Context())
	if err != nil {
		http.Error(w, "Failed to fetch groups", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

func JoinPublicGroupHandler(w http.ResponseWriter, r *http.Request) {
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
		GroupID int `json:"groupID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := db.JoinGroup(r.Context(), req.GroupID, userID)
	if err != nil {
		http.Error(w, "Failed to join group", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func JoinByCodeHandler(w http.ResponseWriter, r *http.Request) {
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
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	g, err := db.JoinGroupByCode(r.Context(), req.Code, userID)
	if err != nil {
		http.Error(w, "Invalid code or failed to join", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

func RemoveMemberHandler(w http.ResponseWriter, r *http.Request) {
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
		GroupID      int `json:"groupID"`
		TargetUserID int `json:"targetUserID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := db.RemoveMember(r.Context(), req.GroupID, userID, req.TargetUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func GetGroupMembersHandler(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.URL.Query().Get("groupID")
	groupID, _ := strconv.Atoi(groupIDStr)
	if groupID == 0 {
		http.Error(w, "Invalid group ID", http.StatusBadRequest)
		return
	}

	members, err := db.GetGroupMembers(r.Context(), groupID)
	if err != nil {
		http.Error(w, "Failed to fetch members", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}

func DeleteGroupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := handler.GetUserIDFromContext(r)
	var req struct {
		GroupID int `json:"groupID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := db.DeleteGroup(r.Context(), req.GroupID, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func LeaveGroupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := handler.GetUserIDFromContext(r)
	var req struct {
		GroupID int `json:"groupID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := db.LeaveGroup(r.Context(), req.GroupID, userID)
	if err != nil {
		http.Error(w, "Failed to leave group", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func RegenerateCodeHandler(w http.ResponseWriter, r *http.Request) {
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
		GroupID int `json:"groupID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	newCode, err := db.RegenerateInviteCode(r.Context(), req.GroupID, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"code": newCode})
}
