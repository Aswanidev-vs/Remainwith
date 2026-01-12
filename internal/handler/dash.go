package handler

import (
	"html/template"
	"net/http"

	"github.com/justinas/nosurf"
)

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	// Get user claims from context
	claims, ok := UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract user name from claims
	name, ok := claims["name"].(string)
	if !ok {
		name = "User" // Fallback if name not found
	}

	sessionID, _ := claims["session_id"].(string)

	// Data to pass to template
	data := struct {
		Name      string
		CSRFToken string
		SessionID string
	}{
		Name:      name,
		CSRFToken: nosurf.Token(r),
		SessionID: sessionID,
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Auth check
	_, err := r.Cookie("auth_token")
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	// Parse and execute template
	tmpl, err := template.ParseFiles("frontend/dashboard.tmpl")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}
