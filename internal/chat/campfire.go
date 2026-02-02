package chat

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"Remainwith/internal/handler"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/golang-jwt/jwt/v5"
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

	// Generate a temporary token for the system/test user
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 1.0,
		"exp":     time.Now().Add(time.Minute).Unix(),
	})
	tokenString, err := token.SignedString(handler.JWTKey)
	if err != nil {
		log.Printf("Failed to sign token: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	c, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://localhost:8080/ws?token=%s", tokenString), nil)
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
