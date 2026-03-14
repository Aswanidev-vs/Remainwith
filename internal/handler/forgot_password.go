package handler

import (
	"Remainwith/db"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/justinas/nosurf"
	"github.com/wneessen/go-mail"
	"golang.org/x/crypto/bcrypt"
)

// ForgotPasswordPageHandler renders the forgot password form.
func ForgotPasswordPageHandler(w http.ResponseWriter, r *http.Request) {
	tmplPath := "frontend/forgot_password.tmpl"
	renderTemplate(w, tmplPath, struct{ CSRFToken string }{CSRFToken: nosurf.Token(r)})
}

// ForgotPasswordHandler processes the forgot password request.
func ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	if email == "" {
		// For better UX, you could render the forgot password page again with an error.
		// However, redirecting to success is a simple and secure default to prevent email enumeration.
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}

	// 1. Check if user exists. We proceed even if not to prevent email enumeration.
	user, err := db.GetUserByEmail(r.Context(), email)
	if err != nil {
		log.Printf("Password reset requested for non-existent or error-prone email: %s, err: %v", email, err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}

	// 2. Generate Reset Token
	token, err := generateSecureToken(32)
	if err != nil {
		log.Printf("Failed to generate password reset token: %v", err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}

	// 3. Store token in the database.
	// TODO: You need to create a `password_reset_tokens` table and implement db.CreatePasswordResetToken.
	// Table schema suggestion: (id, user_id, token_hash, expires_at). Storing a hash of the token is more secure.
	expiresAt := time.Now().Add(15 * time.Minute)
	if err := db.CreatePasswordResetToken(r.Context(), user.ID, token, expiresAt); err != nil {
		log.Printf("Failed to store password reset token for user %d: %v", user.ID, err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}

	// 4. Send Email
	// TODO: Ensure these environment variables are set in your .env or system environment
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPortStr := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")

	if smtpHost == "" || smtpPortStr == "" || smtpUser == "" {
		log.Println("SMTP configuration is incomplete. Cannot send password reset email. Please set SMTP_HOST, SMTP_PORT, and SMTP_USER environment variables.")
		// We still redirect to the success page to not leak information about backend configuration.
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}

	m := mail.NewMsg()
	if err := m.From(smtpUser); err != nil { // Use a "From" address your provider allows
		log.Printf("Failed to set From address for password reset: %v", err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}
	if err := m.To(email); err != nil {
		log.Printf("Failed to set To address for password reset: %v", err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}
	m.Subject("Remainwith - Password Reset")

	resetLink := fmt.Sprintf("http://%s/reset-password?token=%s", r.Host, token)

	// Create email body from template
	var body bytes.Buffer
	tmpl, err := template.ParseFiles("frontend/password_reset.tmpl")

		templateData := struct {
			ResetLink string
			Year      int
		}{
			ResetLink: resetLink,
			Year:      time.Now().Year(),
		}
		templateErr := tmpl.Execute(&body, templateData)
		if err != nil || templateErr != nil {
			if err != nil {
				log.Printf("Failed to parse email template: %v", err)
			}
			if templateErr != nil {
				log.Printf("Failed to execute email template: %v", templateErr)
			}
			// Fallback to simple text email
			fallbackBody := fmt.Sprintf("A password reset was requested for your account. If you did not request this, you can ignore this email.\nClick the link below to reset your password. This link is valid for 15 minutes.\n%s", resetLink)
			m.SetBodyString(mail.TypeTextPlain, fallbackBody)
		} else {
			m.SetBodyString(mail.TypeTextHTML, body.String())
		}

	port, err := strconv.Atoi(smtpPortStr)
	if err != nil {
		log.Printf("Invalid SMTP port in config: %s", smtpPortStr)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}
	// Create a new SMTP client
	c, err := mail.NewClient(smtpHost, mail.WithPort(port), mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithUsername(smtpUser), mail.WithPassword(smtpPass),
		mail.WithTLSPolicy(mail.TLSOpportunistic))
	if err != nil {
		log.Printf("Failed to create mail client: %v", err)
		http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
		return
	}
	// Dial and send the email
	if err := c.DialAndSend(m); err != nil {
		log.Printf("Failed to send password reset email to %s: %v", email, err)
		// Don't expose email sending failure to the user.
	} else {
		log.Printf("Password reset email sent to: %s", email)
	}

	http.Redirect(w, r, "/forgot-password-success", http.StatusSeeOther)
}

// ForgotPasswordSuccessPageHandler renders the success message.
func ForgotPasswordSuccessPageHandler(w http.ResponseWriter, r *http.Request) {
	tmplPath := "frontend/forgot_password_success.tmpl"
	renderTemplate(w, tmplPath, nil)
}

// ResetPasswordPageHandler renders the password reset form.
func ResetPasswordPageHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	data := struct {
		CSRFToken string
		Token     string
		Error     string
	}{
		CSRFToken: nosurf.Token(r),
		Token:     token,
		Error:     "",
	}
	tmplPath := "frontend/reset_password.tmpl"
	renderTemplate(w, tmplPath, data)
}

// ResetPasswordHandler processes the new password.
func ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	token := r.FormValue("token")
	password := r.FormValue("password")

	if token == "" || password == "" {
		renderResetPasswordError(w, r, token, "Password cannot be empty.")
		return
	}

	// 1. Validate token
	// TODO: Implement db.GetPasswordResetToken(ctx, token) to retrieve token details.
	resetToken, err := db.GetPasswordResetToken(r.Context(), token)
	if err != nil || resetToken.ExpiresAt.Before(time.Now()) {
		log.Printf("Invalid or expired password reset token used: %s", token)
		renderResetPasswordError(w, r, token, "Invalid or expired password reset link. Please try again.")
		return
	}

	// 2. Update password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Failed to hash new password for user %d: %v", resetToken.UserID, err)
		renderResetPasswordError(w, r, token, "An internal error occurred. Please try again.")
		return
	}

	// TODO: Implement db.UpdateUserPassword(ctx, userID, newPasswordHash)
	if err := db.UpdateUserPassword(r.Context(), resetToken.UserID, string(hashedPassword)); err != nil {
		log.Printf("Failed to update password for user %d: %v", resetToken.UserID, err)
		renderResetPasswordError(w, r, token, "An internal error occurred. Please try again.")
		return
	}

	// 3. Invalidate token
	// TODO: Implement db.DeletePasswordResetToken(ctx, token)
	if err := db.DeletePasswordResetToken(r.Context(), token); err != nil {
		log.Printf("Failed to delete used password reset token %s: %v", token, err)
	}

	log.Printf("Password reset successfully for user %d", resetToken.UserID)
	http.Redirect(w, r, "/login?status=password_reset_success", http.StatusSeeOther)
}

func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func renderResetPasswordError(w http.ResponseWriter, r *http.Request, token, errorMsg string) {
	tmplPath := "frontend/reset_password.tmpl"
	data := struct {
		CSRFToken string
		Token     string
		Error     string
	}{
		CSRFToken: nosurf.Token(r),
		Token:     token,
		Error:     errorMsg,
	}
	w.WriteHeader(http.StatusBadRequest)
	renderTemplate(w, tmplPath, data)
}

func renderTemplate(w http.ResponseWriter, path string, data interface{}) {
	// In your project structure, templates seem to be standalone files in frontend/
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		log.Printf("Error parsing template %s: %v", path, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Error executing template: %v", err)
	}
}
