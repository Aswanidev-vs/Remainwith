package handler

import (
	"Remainwith/db"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/justinas/nosurf"
)

func ProfilePageHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := GetUserIDFromContext(r)
	name, _ := claims["name"].(string)
	email, ok := claims["email"].(string)
	if !ok || email == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Fetch interests for both GET and POST
	interests, err := db.GetUserInterests(r.Context(), userID)
	if err != nil {
		log.Printf("Error fetching interests: %v", err)
		interests = []string{}
	}

	if r.Method == http.MethodPost {
		// Handle updating interests
		err := r.ParseForm()
		if err != nil {
			log.Printf("Error parsing form: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify session matches the form session to prevent cross-tab data overwrites
		formSessionID := r.FormValue("session_id")
		sessionID, _ := claims["session_id"].(string)
		if formSessionID == "" || formSessionID != sessionID {
			log.Printf("Session mismatch: Form session '%s' != Current session '%s'. Redirecting to refresh.", formSessionID, sessionID)
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}

		interestsStr := r.FormValue("interests")
		var interests []string
		if interestsStr != "" {
			// Split by comma and trim spaces
			parts := strings.Split(interestsStr, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					interests = append(interests, trimmed)
				}
			}
		}

		err = db.SaveUserInterestsByNames(r.Context(), userID, interests)
		if err != nil {
			log.Printf("Error saving interests: %v", err)
			http.Error(w, "Failed to save interests", http.StatusInternalServerError)
			return
		}

		// Redirect back to profile to show updated interests
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	// GET request: display profile
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	sessionID, _ := claims["session_id"].(string)

	data := struct {
		Name          string
		Email         string
		UserID        int
		SessionID     string
		UserInterests []string
		CSRFToken     string
		Error         string
	}{
		Name:          name,
		Email:         email,
		UserID:        userID,
		SessionID:     sessionID,
		UserInterests: interests,
		CSRFToken:     nosurf.Token(r),
		Error:         "",
	}

	tmpl, err := template.ParseFiles("frontend/profile.tmpl")
	if err != nil {
		log.Printf("Template parsing failed: %v", err)
		http.Error(w, "Template parsing failed", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, data)
}
