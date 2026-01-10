package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"Remainwith/internal/models"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"
)

// Hub manages websocket connections and message broadcasting
type Hub struct {
	// subscribers holds all active websocket connections
	subscribers map[*websocket.Conn]struct{}
	mu          sync.RWMutex

	// broadcast channel for incoming messages
	broadcast chan models.Message

	// register/unregister channels for connections
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
}

// NewHub creates a new websocket hub
func NewHub() *Hub {
	h := &Hub{
		subscribers: make(map[*websocket.Conn]struct{}),
		broadcast:   make(chan models.Message, 256),
		register:    make(chan *websocket.Conn),
		unregister:  make(chan *websocket.Conn),
	}
	go h.run()
	return h
}

// run handles the hub's main loop
func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.subscribers[conn] = struct{}{}
			h.mu.Unlock()
			log.Printf("Client connected. Total subscribers: %d", len(h.subscribers))

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.subscribers[conn]; ok {
				delete(h.subscribers, conn)
				conn.Close(websocket.StatusNormalClosure, "unregistered")
			}
			h.mu.Unlock()
			log.Printf("Client disconnected. Total subscribers: %d", len(h.subscribers))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.subscribers {
				go func(c *websocket.Conn, m models.Message) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					data, err := json.Marshal(m)
					if err != nil {
						log.Printf("Error marshaling message: %v", err)
						return
					}

					err = c.Write(ctx, websocket.MessageText, data)
					if err != nil {
						log.Printf("Error writing to websocket: %v", err)
						h.unregister <- c
					}
				}(conn, msg)
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msg models.Message) {
	select {
	case h.broadcast <- msg:
	default:
		log.Println("Broadcast channel full, dropping message")
	}
}

// HandleConnection handles a new websocket connection
func (h *Hub) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // Allow all origins for development
	})
	if err != nil {
		log.Printf("Websocket accept error: %v", err)
		return
	}

	// Register the connection
	h.register <- conn

	// Handle incoming messages with rate limiting
	limiter := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)

	// Clean up on disconnect
	defer func() {
		h.unregister <- conn
	}()

	// Read messages from client
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

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

		// Set timestamp if not provided
		if msg.CreatedAt.IsZero() {
			msg.CreatedAt = time.Now()
		}

		// Broadcast the message
		h.Broadcast(msg)
	}
}
