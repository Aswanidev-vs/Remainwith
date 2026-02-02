package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"Remainwith/internal/handler"
	"Remainwith/internal/models"

	"github.com/gorilla/websocket"
)

// chatServer enables broadcasting to a set of subscribers.
type chatServer struct {
	// subscriberMessageBuffer controls the max number
	// of messages that can be queued for a subscriber
	// before it is kicked.
	//
	// Defaults to 16.
	subscriberMessageBuffer int

	// logf controls where logs are sent.
	// Defaults to log.Printf.
	logf func(f string, v ...any)

	// serveMux routes the various endpoints to the appropriate handler.
	serveMux http.ServeMux

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{}
}

// newChatServer constructs a chatServer with the defaults.
func newChatServer() *chatServer {
	cs := &chatServer{
		subscriberMessageBuffer: 16,
		logf:                    log.Printf,
		subscribers:             make(map[*subscriber]struct{}),
	}
	cs.serveMux.Handle("/", http.FileServer(http.Dir(".")))
	cs.serveMux.HandleFunc("/subscribe", cs.subscribeHandler)

	return cs
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, closeSlow is called.
type subscriber struct {
	msgs      chan []byte
	closeSlow func()
	userID    int
}

func (cs *chatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cs.serveMux.ServeHTTP(w, r)
}

// subscribeHandler accepts the WebSocket connection and then subscribes
// it to all future messages.
func (cs *chatServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	err := cs.subscribe(w, r, userID)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return
	}
	if err != nil {
		cs.logf("%v", err)
		return
	}
}

// subscribe subscribes the given WebSocket to all broadcast messages.
// It creates a subscriber with a buffered msgs chan to give some room to slower
// connections and then registers the subscriber. It then listens for all messages
// and writes them to the WebSocket. If the context is cancelled or
// an error occurs, it returns and deletes the subscription.
//
// It uses CloseRead to keep reading from the connection to process control
// messages and cancel the context if the connection drops.
func (cs *chatServer) subscribe(w http.ResponseWriter, r *http.Request, userID int) error {
	var mu sync.Mutex
	var c *websocket.Conn
	var closed bool
	s := &subscriber{
		msgs: make(chan []byte, cs.subscriberMessageBuffer),
		closeSlow: func() {
			mu.Lock()
			defer mu.Unlock()
			closed = true
			if c != nil {
				c.Close()
			}
		},
		userID: userID,
	}
	cs.addSubscriber(s)
	defer cs.deleteSubscriber(s)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	c2, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	mu.Lock()
	if closed {
		mu.Unlock()
		return net.ErrClosed
	}
	c = c2
	mu.Unlock()
	defer c.Close()

	ctx := r.Context()

	readErr := make(chan error, 1)
	go func() {
		mh := NewMessageHandler()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}

			var m models.Message
			if err := json.Unmarshal(msg, &m); err != nil {
				cs.logf("failed to unmarshal message: %v", err)
				continue
			}

			m.SenderID = strconv.Itoa(userID)
			m.CreatedAt = time.Now()

			if err := mh.ValidateMessage(&m); err != nil {
				cs.logf("invalid message from user %d: %v", userID, err)
				continue
			}

			cs.routeMessage(m)
		}
	}()

	for {
		select {
		case msg := <-s.msgs:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case err := <-readErr:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// routeMessage routes the msg to specific subscriber or broadcasts if receiver is "all".
func (cs *chatServer) routeMessage(msg models.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	cs.subscribersMu.Lock()
	defer cs.subscribersMu.Unlock()

	targetID, _ := strconv.Atoi(msg.ReceiverID)
	isBroadcast := msg.ReceiverID == "all"

	for s := range cs.subscribers {
		if isBroadcast || s.userID == targetID {
			select {
			case s.msgs <- data:
			default:
				go s.closeSlow()
			}
		}
	}
}

// addSubscriber registers a subscriber.
func (cs *chatServer) addSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	cs.subscribers[s] = struct{}{}
	cs.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (cs *chatServer) deleteSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	delete(cs.subscribers, s)
	cs.subscribersMu.Unlock()
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	// For gorilla, we can use a deadline
	c.SetWriteDeadline(time.Now().Add(timeout))
	return c.WriteMessage(websocket.TextMessage, msg)
}
