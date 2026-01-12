package chat

import (
	"Remainwith/db"
	"Remainwith/internal/handler"
	"html/template"
	"log"
	"net/http"
)

type ChatPageData struct {
	SuggestedUsers []db.Userinfo
}

func ChatPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	userID := handler.GetUserIDFromContext(r)
	if userID == 0 {
		log.Println("ChatPage: User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	suggestedUsers, err := db.GetSuggestedUsers(r.Context(), userID, 10)
	if err != nil {
		log.Printf("ChatPage: Failed to get suggested users for user %d: %v", userID, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	data := ChatPageData{
		SuggestedUsers: suggestedUsers,
	}

	tmpl, err := template.ParseFiles("frontend/chat.tmpl")
	if err != nil {
		http.Error(w, "issue faced for parsing about", http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, data)
}

func ChatHandler(w http.ResponseWriter, r *http.Request) {

}
