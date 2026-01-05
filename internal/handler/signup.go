package handler

import (
	"Remainwith/db"
	"html/template"
	"log"
	"net/http"

	"github.com/justinas/nosurf"
	"golang.org/x/crypto/bcrypt"
)

type Signup struct {
	Name       string
	Email      string
	Password   string
	Repassword string
}

func SignupPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFiles("frontend/signup.tmpl")
	if err != nil {
		http.Error(w, "Template parsing failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		CSRFToken string
		Error     string
	}{
		CSRFToken: nosurf.Token(r),
		Error:     "",
	}
	tmpl.Execute(w, data)
}

func SignupHandler(w http.ResponseWriter, r *http.Request) {

	// Parse the form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	sign := Signup{
		Name:       r.FormValue("name"),
		Email:      r.FormValue("email"),
		Password:   r.FormValue("password"),
		Repassword: r.FormValue("Repassword"),
	}

	if sign.Name == "" || sign.Email == "" || sign.Password == "" || sign.Repassword == "" {
		tmpl, err := template.ParseFiles("frontend/signup.tmpl")
		if err != nil {
			http.Error(w, "Template parsing failed", http.StatusInternalServerError)
			return
		}
		data := struct {
			CSRFToken string
			Error     string
		}{
			CSRFToken: nosurf.Token(r),
			Error:     "All fields are required",
		}
		tmpl.Execute(w, data)
		return
	}
	if sign.Password != sign.Repassword {
		tmpl, err := template.ParseFiles("frontend/signup.tmpl")
		if err != nil {
			http.Error(w, "Template parsing failed", http.StatusInternalServerError)
			return
		}
		data := struct {
			CSRFToken string
			Error     string
		}{
			CSRFToken: nosurf.Token(r),
			Error:     "Passwords do not match",
		}
		tmpl.Execute(w, data)
		return
	}

	exists, err := db.CheckUser(r.Context(), sign.Email)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	if exists {
		// Email already registered → redirect to login
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(sign.Password), bcrypt.DefaultCost)
	if err := db.NewUser(r.Context(), sign.Name, sign.Email, string(hashedPassword)); err != nil {
		log.Println("Error inserting user:", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
