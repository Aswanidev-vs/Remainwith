package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"Remainwith/config"
	"Remainwith/internal/handler"
	"Remainwith/internal/models"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// Hub manages websocket connections and message broadcasting
type Hub struct {
	// subscribers holds all active websocket connections with userID
	subscribers map[*websocket.Conn]int
	mu          sync.RWMutex

	// messages channel for incoming messages
	messages chan models.Message

	// register/unregister channels for connections
	register chan struct {
		conn   *websocket.Conn
		userID int
	}
	unregister chan *websocket.Conn
}

// getUserName retrieves the user's name by ID
func getUserName(ctx context.Context, userID int) string {
	var name string
	err := config.DB.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", userID).Scan(&name)
	if err != nil {
		log.Printf("Error getting user name for ID %d: %v", userID, err)
		return "Unknown User"
	}
	return name
}

// NewHub creates a new websocket hub
func NewHub() *Hub {
	h := &Hub{
		subscribers: make(map[*websocket.Conn]int),
		messages:    make(chan models.Message, 10000),
		register: make(chan struct {
			conn   *websocket.Conn
			userID int
		}),
		unregister: make(chan *websocket.Conn),
	}
	go h.run()
	return h
}

// run handles the hub's main loop
func (h *Hub) run() {
	for {
		select {
		case reg := <-h.register:
			h.mu.Lock()
			h.subscribers[reg.conn] = reg.userID
			h.mu.Unlock()
			log.Printf("Client connected for user %d. Total subscribers: %d", reg.userID, h.getSubscriberCount())
			h.broadcastUserCount()

		case conn := <-h.unregister:
			if h.removeSubscriber(conn) {
				log.Printf("Client disconnected via unregister channel. Total subscribers: %d", h.getSubscriberCount())
				h.broadcastUserCount()
			}

		case msg := <-h.messages:
			h.mu.RLock()
			targetID, _ := strconv.Atoi(msg.ReceiverID)
			senderID, _ := strconv.Atoi(msg.SenderID)
			isBroadcast := msg.ReceiverID == "all"

			for conn, userID := range h.subscribers {
				if (isBroadcast || userID == targetID) && userID != senderID {
					go func(c *websocket.Conn, m models.Message) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()

						// Ensure SenderName is set for group messages
						if m.IsGroup && m.SenderName == "" {
							m.SenderName = getUserName(ctx, senderID)
						}

						data, err := json.Marshal(m)
						if err != nil {
							log.Printf("Error marshaling message: %v", err)
							return
						}

						err = c.Write(ctx, websocket.MessageText, data)
						if err != nil {
							log.Printf("Error writing to websocket: %v", err)
							// On write error, remove the subscriber and broadcast the new count.
							if h.removeSubscriber(c) {
								h.broadcastUserCount()
							}
						}
					}(conn, msg)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// getSubscriberCount safely returns the number of subscribers.
func (h *Hub) getSubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// broadcastUserCount sends the current subscriber count to all connected clients.
func (h *Hub) broadcastUserCount() {
	h.mu.RLock()
	count := len(h.subscribers)
	// Create a copy of connections to use outside the lock
	subscribersCopy := make([]*websocket.Conn, 0, count)
	for conn := range h.subscribers {
		subscribersCopy = append(subscribersCopy, conn)
	}
	h.mu.RUnlock()

	countMsg := map[string]interface{}{
		"type":  "user_count_update",
		"count": count,
	}
	data, err := json.Marshal(countMsg)
	if err != nil {
		log.Printf("Error marshaling user count message: %v", err)
		return
	}

	for _, conn := range subscribersCopy {
		go func(c *websocket.Conn) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Write(ctx, websocket.MessageText, data); err != nil {
				// The read loop for this connection will likely handle the final disconnect.
				// We can be proactive and remove it here, which will trigger another broadcast.
				h.removeSubscriber(c)
			}
		}(conn)
	}
}

// removeSubscriber safely deletes a subscriber from the map and closes the connection.
// It returns true if a subscriber was actually removed.
func (h *Hub) removeSubscriber(conn *websocket.Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[conn]; ok {
		delete(h.subscribers, conn)
		// Close the connection; ignore error if already closed.
		_ = conn.Close(websocket.StatusNormalClosure, "unregistered")
		return true
	}
	return false
}

// SendMessage sends a message to the specified receiver
func (h *Hub) SendMessage(msg models.Message) {
	select {
	case h.messages <- msg:
	default:
		log.Println("Messages channel full, dropping message")
	}
}

// HandleConnection handles a new websocket connection
func (h *Hub) HandleConnection(w http.ResponseWriter, r *http.Request) {
	// Authenticate user
	token := r.URL.Query().Get("token")
	if token == "" {
		if c, err := r.Cookie("auth_token"); err == nil {
			token = c.Value
		}
	}

	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	claims := &jwt.MapClaims{}
	tkn, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return handler.JWTKey, nil
	})
	if err != nil || !tkn.Valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userIDFloat, ok := (*claims)["user_id"].(float64)
	if !ok {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}
	userID := int(userIDFloat)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // Allow all origins for development
	})
	if err != nil {
		log.Printf("Websocket accept error: %v", err)
		return
	}

	// Register the connection with userID
	h.register <- struct {
		conn   *websocket.Conn
		userID int
	}{conn: conn, userID: userID}

	// Start a keep‑alive ping goroutine (optional but improves reliability)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Send a ping; ignore error – if it fails, the read loop will handle closure
				_ = conn.Ping(context.Background())
			case <-r.Context().Done():
				return
			}
		}
	}()

	// Handle incoming messages with rate limiting
	limiter := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)

	// Clean up on disconnect
	defer func() {
		h.unregister <- conn
	}()

	// Read messages from client
	mh := NewMessageHandler()
	for {
		// Set a generous read timeout. The 30s ping handler will keep the connection
		// alive. This timeout is to detect a dead connection where pongs are not received.
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)

		_, data, err := conn.Read(ctx)
		cancel()

		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				return
			}
			log.Printf("Websocket read error: %v", err)
			return
		}

		// Rate limit incoming messages
		if err := limiter.Wait(context.Background()); err != nil {
			log.Printf("Rate limit exceeded: %v", err)
			continue
		}

		// Parse incoming message
		var msg models.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// Set senderID from authenticated user
		msg.SenderID = strconv.Itoa(userID)

		// Set sender name
		msg.SenderName = getUserName(r.Context(), userID)

		// Set timestamp if not provided
		if msg.CreatedAt.IsZero() {
			msg.CreatedAt = time.Now()
		}

		if err := mh.ValidateMessage(&msg); err != nil {
			log.Printf("Invalid message: %v", err)
			continue
		}

		// Send the message to receiver
		h.SendMessage(msg)
	}
}
