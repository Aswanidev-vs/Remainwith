package handler

import (
	"Remainwith/db"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/justinas/nosurf"
	"golang.org/x/crypto/bcrypt"
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
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
			return
		}

		action := r.FormValue("action")
		switch action {
		case "update_interests":
			var selectedInterests []string
			for _, v := range r.Form["interests"] {
				parts := strings.Split(v, ",")
				for _, part := range parts {
					trimmed := strings.TrimSpace(part)
					if trimmed != "" {
						selectedInterests = append(selectedInterests, trimmed)
					}
				}
			}
			err = db.SaveUserInterestsByNames(r.Context(), userID, selectedInterests)
			if err != nil {
				log.Printf("Error saving interests: %v", err)
				errorMsg = "Failed to save interests."
			} else {
				successMsg = "Interests updated successfully."
				interests = selectedInterests
			}

		case "update_profile":
			newName := strings.TrimSpace(r.FormValue("name"))
			newEmail := strings.TrimSpace(r.FormValue("email"))
			if newName == "" || newEmail == "" {
				errorMsg = "Name and email cannot be empty."
			} else {
				err = db.UpdateUserInfo(r.Context(), userID, newName, newEmail)
				if err != nil {
					log.Printf("Error updating user info: %v", err)
					errorMsg = "Failed to update profile. Email might already be taken."
				} else {
					successMsg = "Profile updated successfully."
					name = newName
					email = newEmail
					// Note: JWT claims aren't updated here, user might need to relogin for header reflected change
					// but the local vars are updated for the template render.
				}
			}

		case "update_password":
			currentPassword := r.FormValue("current_password")
			newPassword := r.FormValue("new_password")
			confirmPassword := r.FormValue("confirm_password")

			if newPassword != confirmPassword {
				errorMsg = "New passwords do not match."
			} else if len(newPassword) < 8 {
				errorMsg = "New password must be at least 8 characters long."
			} else {
				// Fetch user to verify current password
				user, err := db.GetUserByEmail(r.Context(), email)
				if err != nil {
					errorMsg = "User not found."
				} else {
					err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword))
					if err != nil {
						errorMsg = "Incorrect current password."
					} else {
						hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
						if err != nil {
							errorMsg = "Failed to process new password."
						} else {
							err = db.UpdateUserPassword(r.Context(), userID, string(hashedPassword))
							if err != nil {
								errorMsg = "Failed to update password."
							} else {
								successMsg = "Password changed successfully."
							}
						}
					}
				}
			}
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
