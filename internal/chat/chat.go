package chat

import (
	"Remainwith/db"
	"Remainwith/internal/handler"
	"context"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// WSMessage defines the structure for all real-time communication
type WSMessage struct {
	Type       string      `json:"type"` // "text", "media", "signal"
	SenderID   int         `json:"senderID"`
	ReceiverID string      `json:"receiverID"`          // "all" for group, or specific UserID string
	Content    string      `json:"content,omitempty"`   // Message text or media URL/Base64
	FileName   string      `json:"fileName,omitempty"`  // For media files
	MediaType  string      `json:"mediaType,omitempty"` // "image", "audio", "video", "file"
	Signal     interface{} `json:"signal,omitempty"`    // WebRTC SDP or ICE Candidates
}

type ChatPageData struct {
	SuggestedUsers []db.Userinfo
	UserGroups     []db.Group
	PublicGroups   []db.Group
	UserID         int
}

// Global connection manager for routing 1-to-1 messages and calls
var (
	clients   = make(map[int]*websocket.Conn)
	clientsMu sync.RWMutex
)

func ChatPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		log.Println("ChatPage: User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	suggestedUsers, err := db.GetSuggestedUsers(r.Context(), userID, 10)
	if err != nil {
		log.Printf("ChatPage: Failed to get suggested users for user %d: %v", userID, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	userGroups, err := db.GetUserGroups(r.Context(), userID)
	if err != nil {
		log.Printf("ChatPage: Failed to get user groups for user %d: %v", userID, err)
	}

	publicGroups, err := db.GetPublicGroups(r.Context())
	if err != nil {
		log.Printf("ChatPage: Failed to get public groups: %v", err)
	}

	data := ChatPageData{
		UserID:         userID,
		SuggestedUsers: suggestedUsers,
		UserGroups:     userGroups,
		PublicGroups:   publicGroups,
	}

	tmpl, err := template.ParseFiles("frontend/chat.tmpl")
	if err != nil {
		http.Error(w, "issue faced for parsing about", http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, data)
}

func ChatHandler(w http.ResponseWriter, r *http.Request) {
	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Logic for production: restrict origins
	})
	if err != nil {
		log.Printf("ChatHandler: Failed to accept websocket: %v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "connection closed")

	// Register client
	clientsMu.Lock()
	clients[userID] = c
	clientsMu.Unlock()

	log.Printf("User %d connected to chat", userID)

	defer func() {
		clientsMu.Lock()
		delete(clients, userID)
		clientsMu.Unlock()
		log.Printf("User %d disconnected", userID)
	}()

	ctx := r.Context()
	for {
		var msg WSMessage
		err := wsjson.Read(ctx, c, &msg)
		if err != nil {
			log.Printf("Read error for user %d: %v", userID, err)
			break
		}

		msg.SenderID = userID // Ensure sender ID is authentic

		// Handle Routing
		if msg.ReceiverID == "all" {
			// Group Chat (Campfire) Broadcast
			broadcast(ctx, msg)
		} else if len(msg.ReceiverID) > 6 && msg.ReceiverID[:6] == "group_" {
			// Specific Group Broadcast
			groupID, _ := strconv.Atoi(msg.ReceiverID[6:])
			if groupID > 0 {
				broadcastToGroup(ctx, groupID, msg)
			}
		} else {
			// 1-to-1 Chat, Media Sharing, or A/V Signaling
			routePrivateMessage(ctx, msg)
		}
	}
}

func broadcast(ctx context.Context, msg WSMessage) {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	for _, conn := range clients {
		_ = wsjson.Write(ctx, conn, msg)
	}
}

func broadcastToGroup(ctx context.Context, groupID int, msg WSMessage) {
	members, err := db.GetGroupMembers(ctx, groupID)
	if err != nil {
		log.Printf("broadcastToGroup: failed to get members for group %d: %v", groupID, err)
		return
	}

	clientsMu.RLock()
	defer clientsMu.RUnlock()

	for _, m := range members {
		if conn, ok := clients[m.UserID]; ok {
			_ = wsjson.Write(ctx, conn, msg)
		}
	}
}

func routePrivateMessage(ctx context.Context, msg WSMessage) {
	targetID, err := strconv.Atoi(msg.ReceiverID)
	if err != nil {
		log.Printf("routePrivateMessage: invalid ReceiverID %s", msg.ReceiverID)
		return
	}

	clientsMu.RLock()
	targetConn, ok := clients[targetID]
	clientsMu.RUnlock()

	if ok {
		_ = wsjson.Write(ctx, targetConn, msg)
	} else {
		log.Printf("routePrivateMessage: user %d not online", targetID)
	}
}
