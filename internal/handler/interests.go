package handler

import (
	"Remainwith/db"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// CheckOnboardingHandler returns whether the onboarding dialog should be shown.
func CheckOnboardingHandler(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r)
	if userID == 0 {
		log.Println("CheckOnboarding: User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	show, err := db.ShouldShowOnboarding(r.Context(), userID)
	if err != nil {
		log.Printf("CheckOnboarding: DB error for user %d: %v", userID, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"show": show})
}

// GetInterestsHandler returns the list of all interests.
func GetInterestsHandler(w http.ResponseWriter, r *http.Request) {
	interests, err := db.GetAllInterests(r.Context())
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(interests)
}

// SaveInterestsHandler saves the user's selected interests.
func SaveInterestsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := GetUserIDFromContext(r)
	if userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		InterestNames []string `json:"interest_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate max 5
	if len(req.InterestNames) > 5 {
		http.Error(w, "Too many interests selected", http.StatusBadRequest)
		return
	}

	if err := db.SaveUserInterestsByNames(r.Context(), userID, req.InterestNames); err != nil {
		http.Error(w, "Failed to save interests", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Helper to extract UserID from context (assuming JWTMiddleware sets it)
func GetUserIDFromContext(r *http.Request) int {
	claims, ok := UserFromContext(r.Context())
	if !ok {
		return 0
	}

	// Try to get user_id as float64 (standard JSON number)
	if idFloat, ok := claims["user_id"].(float64); ok {
		return int(idFloat)
	}

	// Try to get user_id as string (sometimes sent as string)
	if idStr, ok := claims["user_id"].(string); ok {
		if idInt, err := strconv.Atoi(idStr); err == nil {
			return idInt
		}
	}

	return 0
}
