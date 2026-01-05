package message

import (
	"Remainwith/db"
	"Remainwith/internal/handler"
	"html/template"
	"net/http"
	"strconv"

	"github.com/justinas/nosurf"
)

type Journal struct {
	Title string
	Desc  string
}

func JournalPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFiles("frontend/journal.tmpl")
	if err != nil {
		http.Error(w, "issue faced for parsing journal", http.StatusInternalServerError)
		return
	}

	// Get user claims from context
	claims, ok := handler.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract user_id from claims
	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}
	userID := int(userIDFloat)

	journals, err := db.GetJournalsByUserID(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to fetch journals", http.StatusInternalServerError)
		return
	}

	data := struct {
		CSRFToken string
		Journals  []db.Journal
	}{
		CSRFToken: nosurf.Token(r),
		Journals:  journals,
	}
	tmpl.Execute(w, data)

}

func JournalHandler(w http.ResponseWriter, r *http.Request) {

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}
	journal := &Journal{
		Title: r.FormValue("title"),
		Desc:  r.FormValue("entry"),
	}
	if journal.Title == "" || journal.Desc == "" {
		http.Error(w, "all fields are required", http.StatusBadRequest)
		return
	}

	// Get user claims from context
	claims, ok := handler.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract user_id from claims
	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}
	userID := int(userIDFloat)

	_, err := db.NewJournal(r.Context(), userID, journal.Title, journal.Desc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/journal", http.StatusSeeOther)

}

func UpdateJournalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	desc := r.FormValue("entry")
	if title == "" || desc == "" {
		http.Error(w, "all fields are required", http.StatusBadRequest)
		return
	}

	// Get user claims
	claims, ok := handler.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}
	userID := int(userIDFloat)

	// Update journal
	err = db.UpdateJournal(r.Context(), id, userID, title, desc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/journal", http.StatusSeeOther)
}

func DeleteJournalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	idStr := r.URL.Path[len("/journal/delete/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Get user claims
	claims, ok := handler.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}
	userID := int(userIDFloat)

	// Delete journal
	err = db.DeleteJournal(r.Context(), id, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/journal", http.StatusSeeOther)
}
