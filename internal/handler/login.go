package handler

import (
	"Remainwith/db"
	"html/template"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/justinas/nosurf"
	"golang.org/x/crypto/bcrypt"
)

type Login struct {
	Email    string
	Password string
}

func LoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.ParseFiles("frontend/login.tmpl")
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
func LoginHandler(w http.ResponseWriter, r *http.Request) {

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	req := Login{
		Email:    r.FormValue("email"),
		Password: r.FormValue("password"),
	}

	user, err := db.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		// User not found or other error
		tmpl, tmplErr := template.ParseFiles("frontend/login.tmpl")
		if tmplErr != nil {
			http.Error(w, "Template parsing failed", http.StatusInternalServerError)
			return
		}
		data := struct {
			CSRFToken string
			Error     string
		}{
			CSRFToken: nosurf.Token(r),
			Error:     "Invalid email or password",
		}
		tmpl.Execute(w, data)
		return
	}

	if bcrypt.CompareHashAndPassword(
		[]byte(user.Password),
		[]byte(req.Password),
	) != nil {
		// Password mismatch
		tmpl, tmplErr := template.ParseFiles("frontend/login.tmpl")
		if tmplErr != nil {
			http.Error(w, "Template parsing failed", http.StatusInternalServerError)
			return
		}
		data := struct {
			CSRFToken string
			Error     string
		}{
			CSRFToken: nosurf.Token(r),
			Error:     "Invalid email or password",
		}
		tmpl.Execute(w, data)
		return
	}

	claims := jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"name":    user.Name,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(JWTKey)
	if err != nil {
		http.Error(w, "Token generation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    tokenString,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   false, // MUST be false for localhost
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
