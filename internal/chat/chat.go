package chat

import (
	"html/template"
	"net/http"
)

func ChatPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFiles("frontend/chat.tmpl")
	if err != nil {
		http.Error(w, "issue faced for parsing about", http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, nil)
}

func ChatHandler(w http.ResponseWriter, r *http.Request) {

}
