package handler

import (
	"Remainwith/config"
	"Remainwith/internal/call/video"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
)

type VideoCallData struct {
	UserID   int
	UserName string
}

func VideoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	userID := GetUserIDFromContext(r)
	if userID == 0 {
		log.Println("VideoHandler: User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user name
	var userName string
	err := config.DB.QueryRow(r.Context(), "SELECT name FROM users WHERE id = $1", userID).Scan(&userName)
	if err != nil {
		log.Printf("VideoHandler: Failed to get user name for ID %d: %v", userID, err)
		userName = "Guest"
	}

	data := VideoCallData{
		UserID:   userID,
		UserName: userName,
	}

	tmpl, err := template.ParseFiles("frontend/videocall.tmpl")
	if err != nil {
		http.Error(w, "Template parsing failed", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Template execution failed", http.StatusInternalServerError)
		return
	}
}

func CreateRoomHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Unauthorized: Invalid user ID in token", http.StatusUnauthorized)
		return
	}
	userID := strconv.Itoa(int(userIDFloat))

	userName, ok := claims["name"].(string)
	if !ok || userName == "" {
		http.Error(w, "Unauthorized: Invalid user name in token", http.StatusUnauthorized)
		return
	}

	var req struct {
		RoomName string `json:"room_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	room, err := video.CreateRoom(r.Context(), req.RoomName, userID, userName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"room_id":   room.ID,
		"room_name": room.Name,
	})
}

func JoinRoomHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RoomName            string `json:"room_name"`
		ParticipantName     string `json:"participant_name"`
		ParticipantIdentity string `json:"participant_identity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	roomID, err := video.JoinRoom(req.RoomName, req.ParticipantName, req.ParticipantIdentity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"room_id": roomID,
	})

}

func ListParticipantsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	roomName := r.URL.Query().Get("room_name")
	if roomName == "" {
		http.Error(w, "room_name required", http.StatusBadRequest)
		return
	}
	participants, err := video.ListParticipants(r.Context(), roomName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"participants": participants,
	})
}

func CreateRoomWithIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Unauthorized: Invalid user ID in token", http.StatusUnauthorized)
		return
	}
	userID := strconv.Itoa(int(userIDFloat))

	userName, ok := claims["name"].(string)
	if !ok || userName == "" {
		http.Error(w, "Unauthorized: Invalid user name in token", http.StatusUnauthorized)
		return
	}

	var req struct {
		RoomID   string `json:"room_id"`
		RoomName string `json:"room_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.RoomID == "" || req.RoomName == "" {
		http.Error(w, "room_id and room_name required", http.StatusBadRequest)
		return
	}

	room, err := video.CreateRoomWithID(r.Context(), req.RoomID, req.RoomName, userID, userName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"room_id":   room.ID,
		"room_name": room.Name,
	})
}
