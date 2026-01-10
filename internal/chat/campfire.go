package chat

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func CampfirePageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFiles("frontend/campfire.tmpl")
	if err != nil {
		http.Error(w, "issue faced for parsing about", http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, nil)
}

func campfireHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080/ws", nil)
	if err != nil {
		log.Printf("Failed to connect to websocket: %v", err)
		http.Error(w, "Failed to connect to chat", http.StatusInternalServerError)
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// Send a test message
	testMsg := map[string]interface{}{
		"senderID":   "system",
		"receiverID": "all",
		"content":    "Welcome to the campfire chat!",
	}

	err = wsjson.Write(ctx, c, testMsg)
	if err != nil {
		log.Printf("Failed to send message: %v", err)
		return
	}

	// Read response
	var response interface{}
	err = wsjson.Read(ctx, c, &response)
	if err != nil {
		log.Printf("Failed to read response: %v", err)
		return
	}

	log.Printf("Received response: %v", response)
}
