package handler

import (
	"fmt"
	"net/http"
	"strings"
)

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	// Get the session data from the client-side cookie
	sessionCookie, err := r.Cookie("session_data")
	if err != nil {
		// No session cookie, redirect to login
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse session data (user_id|session_id|email)
	parts := strings.Split(sessionCookie.Value, "|")
	if len(parts) != 3 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := parts[0]
	sessionID := parts[1]

	// Verify this matches the server-side session
	if claims, ok := UserFromContext(r.Context()); ok {
		currentSessionID, _ := claims["session_id"].(string)
		currentUserID := fmt.Sprintf("%v", claims["user_id"])

		// Only allow logout if session matches
		if currentSessionID != sessionID || currentUserID != userID {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
	}

	// Clear both cookies
	authCookie := &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	}

	sessionDataCookie := &http.Cookie{
		Name:     "session_data",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
	}

	http.SetCookie(w, authCookie)
	http.SetCookie(w, sessionDataCookie)

	// Prevent caching of logout response
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
