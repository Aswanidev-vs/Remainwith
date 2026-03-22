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

	var successMsg string
	var errorMsg string

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("Error parsing form: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify session matches
		formSessionID := r.FormValue("session_id")
		sessionID, _ := claims["session_id"].(string)
		if formSessionID == "" || formSessionID != sessionID {
			log.Printf("Session mismatch")
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}

		action := r.FormValue("action")
		switch action {
		case "update_interests":
			interestsStr := r.FormValue("interests")
			var interestsList []string
			if interestsStr != "" {
				interestsList = strings.Split(interestsStr, ",")
				// Trim whitespace from each interest
				for i := range interestsList {
					interestsList[i] = strings.TrimSpace(interestsList[i])
				}
				// Filter out empty strings
				var filteredInterests []string
				for _, i := range interestsList {
					if i != "" {
						filteredInterests = append(filteredInterests, i)
					}
				}
				interestsList = filteredInterests
			}
			err = db.SaveUserInterestsByNames(r.Context(), userID, interestsList)
			if err != nil {
				log.Printf("Error saving interests: %v", err)
				errorMsg = "Failed to save interests. Please try again."
			} else {
				successMsg = "Your interests have been saved successfully!"
				interests = interestsList // Update displayed interests
			}
		default:
			log.Printf("Unknown action: %s", action)
		}
	}

	// GET request or after POST processing: display profile
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
		Success       string
	}{
		Name:          name,
		Email:         email,
		UserID:        userID,
		SessionID:     sessionID,
		UserInterests: interests,
		CSRFToken:     nosurf.Token(r),
		Error:         errorMsg,
		Success:       successMsg,
	}

	tmpl, err := template.ParseFiles("frontend/profile.tmpl")
	if err != nil {
		log.Printf("Template parsing failed: %v", err)
		http.Error(w, "Template parsing failed", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, data)
}
