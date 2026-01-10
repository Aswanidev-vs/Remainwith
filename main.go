package main

import (
	"Remainwith/config"
	"Remainwith/db"
	"Remainwith/internal/about"
	"Remainwith/internal/chat"
	"Remainwith/internal/handler"
	"Remainwith/internal/message"
	"Remainwith/internal/ws"
	"context"
	"log"
	"net/http"
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

	router.Handle("GET /campfire/chat", http.HandlerFunc(chat.ChatPageHandler))

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

	logger := handler.Logger(router)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: logger,
		// ReadTimeout:  10 * time.Second,
		// WriteTimeout: 10 * time.Second,
		// IdleTimeout:  60 * time.Second,
	}

	log.Println("Server listening on http://localhost:8080")
	log.Fatal(srv.ListenAndServe())
}
