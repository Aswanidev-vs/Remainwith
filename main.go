package main

import (
	"Remainwith/config"
	"Remainwith/db"
	"Remainwith/internal/about"
	"Remainwith/internal/chat"
	"Remainwith/internal/chat_history"
	"Remainwith/internal/handler"
	"Remainwith/internal/message"
	"Remainwith/internal/sfu"
	"Remainwith/internal/signaling"
	"Remainwith/internal/ws"
	"context"
	"log"
	"net/http"
	"time"
)

func main() {
	config.Init()

	handler.InitJWT()

	if err := config.InitDB(); err != nil {
		log.Fatal("Database connection failed:", err)
	}

	// Seed interests if they don't exist
	if err := db.SeedInterests(context.Background()); err != nil {
		log.Println("Warning: Failed to seed interests:", err)
	}
	// Create password reset tokens table if it doesn't exist
	if err := db.CreatePasswordResetTable(context.Background()); err != nil {
		log.Println("Warning: Failed to create password_reset_tokens table:", err)
	}

	// Add new columns to users table if they don't exist
	_, _ = config.DB.Exec(context.Background(), `
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email_notifications BOOLEAN DEFAULT TRUE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS privacy_visibility TEXT DEFAULT 'public';
	`)

	// Initialize websocket hub
	hub := ws.NewHub()

	router := http.NewServeMux()

	router.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("./assets"))))

	router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("frontend/static/"))))

	router.HandleFunc("/", handler.IndexHandler)

	router.Handle("GET /signup", handler.CSRFMiddleware()(http.HandlerFunc(handler.SignupPageHandler)))

	router.HandleFunc("POST /signup", handler.SignupHandler)

	router.Handle("GET /login", handler.CSRFMiddleware()(http.HandlerFunc(handler.LoginPageHandler)))

	// router.Handle("POST /login", handler.CSRFMiddleware()(http.HandlerFunc(handler.LoginHandler)))
	router.HandleFunc("POST /login", handler.LoginHandler)

	// Forgot Password routes
	// Note: The CSRF middleware is applied to GET handlers that render forms
	// and POST handlers that process them.
	router.Handle("GET /forgot-password", handler.CSRFMiddleware()(http.HandlerFunc(handler.ForgotPasswordPageHandler)))
	router.Handle("POST /forgot-password", handler.CSRFMiddleware()(http.HandlerFunc(handler.ForgotPasswordHandler)))
	router.HandleFunc("GET /forgot-password-success", handler.ForgotPasswordSuccessPageHandler)
	router.Handle("GET /reset-password", handler.CSRFMiddleware()(http.HandlerFunc(handler.ResetPasswordPageHandler)))
	router.Handle("POST /reset-password", handler.CSRFMiddleware()(http.HandlerFunc(handler.ResetPasswordHandler)))

	router.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.DashboardHandler)).ServeHTTP(w, r)
	})

	router.Handle("GET /journal", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(message.JournalPageHandler))))

	router.Handle("POST /journal", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(message.JournalHandler))))

	router.Handle("POST /journal/update/{id}", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(message.UpdateJournalHandler))))

	router.Handle("POST /journal/delete/{id}", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(message.DeleteJournalHandler))))

	router.HandleFunc("POST /logout", handler.LogoutHandler)

	router.Handle("GET /about", http.HandlerFunc(about.AboutpageHandler))

	router.Handle("GET /campfire", http.HandlerFunc(chat.CampfirePageHandler))

	router.Handle("GET /campfire/chat", handler.JWTMiddleware(http.HandlerFunc(chat.ChatPageHandler)))
	router.Handle("GET /api/chat/history", handler.JWTMiddleware(http.HandlerFunc(chat_history.ChatHistoryHandler)))
	router.Handle("POST /api/chat/delete", handler.JWTMiddleware(http.HandlerFunc(chat_history.DeleteMessageHandler)))

	// Interests API routes
	router.HandleFunc("GET /api/interests", handler.GetInterestsHandler)
	router.HandleFunc("POST /api/interests", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.SaveInterestsHandler)).ServeHTTP(w, r)
	})
	router.HandleFunc("GET /api/onboarding/check", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.CheckOnboardingHandler)).ServeHTTP(w, r)
	})

	// Websocket routes
	router.HandleFunc("/ws", hub.HandleConnection)
	router.Handle("GET /ws/chat", handler.JWTMiddleware(http.HandlerFunc(chat.ChatHandler)))

	router.Handle("GET /campfire/video", handler.JWTMiddleware(http.HandlerFunc(handler.VideoHandler)))

	// Signaling WebSocket
	signalingServer := signaling.NewSignalingServer()
	router.HandleFunc("/ws/signaling", signalingServer.HandleConnection)

	// SFU WebSocket
	sfuServer := sfu.New(context.Background(), sfu.DefaultOptions())
	router.Handle("/ws/sfu", sfuServer)

	// Video API routes
	router.HandleFunc("POST /api/video/create-room", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.CreateRoomHandler)).ServeHTTP(w, r)
	})
	router.HandleFunc("POST /api/video/create-room-with-id", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.CreateRoomWithIDHandler)).ServeHTTP(w, r)
	})
	router.HandleFunc("POST /api/video/join-room", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.JoinRoomHandler)).ServeHTTP(w, r)
	})
	router.HandleFunc("GET /api/video/participants", func(w http.ResponseWriter, r *http.Request) {
		handler.JWTMiddleware(http.HandlerFunc(handler.ListParticipantsHandler)).ServeHTTP(w, r)
	})

	router.Handle("/settings/", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(handler.SettingsPageHandler))))
	router.Handle("/profile/", handler.JWTMiddleware(handler.CSRFMiddleware()(http.HandlerFunc(handler.ProfilePageHandler))))

	// Start session cleanup goroutine
	// Initialize Group Tables
	if err := db.InitGroupTables(context.Background()); err != nil {
		log.Fatalf("Failed to initialize group tables: %v", err)
	}
	if err := db.InitMessageTable(context.Background()); err != nil {
		log.Fatalf("Failed to initialize message tables: %v", err)
	}

	// Group API Routes
	router.Handle("/api/groups/create", handler.JWTMiddleware(http.HandlerFunc(chat.CreateGroupHandler)))
	router.Handle("/api/groups/my", handler.JWTMiddleware(http.HandlerFunc(chat.GetMyGroupsHandler)))
	router.Handle("/api/groups/public", handler.JWTMiddleware(http.HandlerFunc(chat.GetPublicGroupsHandler)))
	router.Handle("/api/groups/join", handler.JWTMiddleware(http.HandlerFunc(chat.JoinPublicGroupHandler)))
	router.Handle("/api/groups/join-code", handler.JWTMiddleware(http.HandlerFunc(chat.JoinByCodeHandler)))
	router.Handle("/api/groups/remove-member", handler.JWTMiddleware(http.HandlerFunc(chat.RemoveMemberHandler)))
	router.Handle("/api/groups/delete", handler.JWTMiddleware(http.HandlerFunc(chat.DeleteGroupHandler)))
	router.Handle("/api/groups/leave", handler.JWTMiddleware(http.HandlerFunc(chat.LeaveGroupHandler)))
	router.Handle("/api/groups/members", handler.JWTMiddleware(http.HandlerFunc(chat.GetGroupMembersHandler)))
	router.Handle("/api/groups/regenerate-code", handler.JWTMiddleware(http.HandlerFunc(chat.RegenerateCodeHandler)))

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			// Clean up inactive SFU rooms
			sfuServer.CleanupInactiveRooms()
			// Clean up inactive signaling rooms
			signaling.GetRoomManager().CleanupInactiveRooms(30 * time.Minute)
		}
	}()

	logger := handler.Logger(router)
	srv := &http.Server{
		Addr:    ":8080", // Change to a different port
		Handler: logger,
		// ReadTimeout:  10 * time.Second,
		// WriteTimeout: 10 * time.Second,
		// IdleTimeout:  60 * time.Second,
	}

	log.Println("Server listening on http://localhost:8080")

	log.Fatal(srv.ListenAndServe())
}
