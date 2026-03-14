package handler

import (
	"Remainwith/db"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/justinas/nosurf"
)

func SettingsPageHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := GetUserIDFromContext(r)
	email, ok := claims["email"].(string)
	if !ok || email == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Fetch latest user info from DB
	user, err := db.GetUserByEmail(r.Context(), email)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
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
		case "update_profile":
			newName := strings.TrimSpace(r.FormValue("name"))
			newEmail := strings.TrimSpace(r.FormValue("email"))
			if newName == "" || newEmail == "" {
				errorMsg = "Name and email cannot be empty."
			} else {
				err = db.UpdateUserInfo(r.Context(), userID, newName, newEmail)
				if err != nil {
					log.Printf("Error updating profile: %v", err)
					errorMsg = "Failed to update profile."
				} else {
					successMsg = "Profile updated successfully."
					user.Name = newName
					user.Email = newEmail
				}
			}

		case "update_preferences":
			emailNotifications := r.FormValue("email_notifications") == "on"
			privacyVisibility := r.FormValue("privacy_visibility")
			err = db.UpdateUserSettings(r.Context(), userID, emailNotifications, privacyVisibility)
			if err != nil {
				log.Printf("Error updating settings: %v", err)
				errorMsg = "Failed to update preferences."
			} else {
				successMsg = "Preferences updated successfully."
				user.EmailNotifications = emailNotifications
				user.PrivacyVisibility = privacyVisibility
			}

		case "delete_account":
			err = db.DeleteUserAccount(r.Context(), userID)
			if err != nil {
				log.Printf("Error deleting account: %v", err)
				errorMsg = "Failed to delete account."
			} else {
				// Clear cookie and redirect to signup or landing
				http.SetCookie(w, &http.Cookie{
					Name:   "auth_token",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
				http.Redirect(w, r, "/signup", http.StatusSeeOther)
				return
			}
		}
	}

	interests, _ := db.GetUserInterests(r.Context(), userID)

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	data := struct {
		User          *db.Userinfo
		UserInterests []string
		CSRFToken     string
		Error         string
		Success       string
		SessionID     string
	}{
		User:          user,
		UserInterests: interests,
		CSRFToken:     nosurf.Token(r),
		Error:         errorMsg,
		Success:       successMsg,
		SessionID:     claims["session_id"].(string),
	}

	tmpl, err := template.ParseFiles("frontend/settings.tmpl")
	if err != nil {
		log.Printf("Template failed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, data)
}
