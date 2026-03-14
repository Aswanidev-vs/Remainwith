package db

import (
	"Remainwith/config"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Userinfo struct {
	ID       int
	Name     string
	Email    string
	Password string // store hashed password
}

type Journal struct {
	ID        int
	UserID    int
	Title     string
	Desc      string
	CreatedAt time.Time // or time.Time, but for simplicity string
}

type PasswordResetToken struct {
	ID        int
	UserID    int
	TokenHash string
	ExpiresAt time.Time
}

func GetUserByEmail(ctx context.Context, email string) (*Userinfo, error) {
	user := &Userinfo{}

	err := config.DB.QueryRow(
		ctx,
		`SELECT id, name, email, password
         FROM users
         WHERE email = $1`,
		email,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Password,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}

	return user, nil
}

func NewUser(ctx context.Context, name, email, password string) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := config.DB.Exec(
		ctx,
		`INSERT INTO users (name, email, password, created_at)
         VALUES ($1, $2, $3, NOW())`,
		name, email, password,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// 23505 = unique_violation
			if pgErr.Code == "23505" {
				return fmt.Errorf("email already exists")
			}
		}
		return err
	}

	return nil
}

// func NewUser(name, email, password string) (int, error) {
// 	var id int

// 	err := config.DB.QueryRow(
// 		`INSERT INTO users (name, email, password)
// 		 VALUES ($1, $2, $3)
// 		 RETURNING id`,
// 		name, email, password,
// 	).Scan(&id)

// 	return id, err
// }

// func CheckUser(email string) (bool, error) {
// 	var id int
// 	err := config.DB.QueryRow(
// 		"SELECT id FROM users WHERE email = $1",
// 		email,
// 	).Scan(&id)

// 	if err != nil {
// 		if err == sql.ErrNoRows {
// 			return false, nil
// 		}
// 		return false, err
// 	}

// 	return true, nil
// }

func CheckUser(ctx context.Context, email string) (bool, error) {
	var exists bool

	err := config.DB.QueryRow(
		ctx,
		`SELECT EXISTS (
            SELECT 1 FROM users WHERE email = $1
        )`,
		email,
	).Scan(&exists)

	return exists, err
}

// func NewJournal(ctx context.Context, userID int, title string, desc string) error {

// 	if config.DB == nil {
// 		return fmt.Errorf("database not initialized")
// 	}

// 	_, err := config.DB.Exec(
// 		ctx,
// 		`INSERT INTO journal (user_id, title, description)
// 		 VALUES ($1, $2, $3)`,
// 		userID,
// 		title,
// 		desc,
// 	)

// 	return err
// }

func GetJournalsByUserID(ctx context.Context, userID int) ([]Journal, error) {
	if config.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := config.DB.Query(
		ctx,
		`SELECT id, user_id, title, "desc", created_at
         FROM journal
         WHERE user_id = $1
         ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query journals: %w", err)
	}
	defer rows.Close()

	var journals []Journal
	for rows.Next() {
		var j Journal
		err := rows.Scan(&j.ID, &j.UserID, &j.Title, &j.Desc, &j.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan journal: %w", err)
		}
		journals = append(journals, j)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return journals, nil
}

func UpdateJournal(ctx context.Context, id, userID int, title, description string) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	if title == "" || description == "" {
		return fmt.Errorf("title and description are required")
	}

	_, err := config.DB.Exec(
		ctx,
		`UPDATE journal SET title = $1, "desc" = $2 WHERE id = $3 AND user_id = $4`,
		title, description, id, userID,
	)

	if err != nil {
		return fmt.Errorf("failed to update journal: %w", err)
	}

	return nil
}

func DeleteJournal(ctx context.Context, id, userID int) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := config.DB.Exec(
		ctx,
		`DELETE FROM journal WHERE id = $1 AND user_id = $2`,
		id, userID,
	)

	if err != nil {
		return fmt.Errorf("failed to delete journal: %w", err)
	}

	return nil
}
func NewJournal(ctx context.Context, userID int, title, description string) (int, error) {
	if config.DB == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	if title == "" || description == "" {
		return 0, fmt.Errorf("title and description are required")
	}

	var journalID int

	err := config.DB.QueryRow(
		ctx,
		`INSERT INTO journal (user_id, title, "desc")
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		userID, title, description,
	).Scan(&journalID)

	if err != nil {
		return 0, fmt.Errorf("failed to insert journal: %w", err)
	}

	return journalID, nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func CreatePasswordResetToken(ctx context.Context, userID int, token string, expiresAt time.Time) error {
	tokenHash := hashToken(token)
	_, err := config.DB.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3) ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at`,
		userID, tokenHash, expiresAt)
	return err
}

func GetPasswordResetToken(ctx context.Context, token string) (*PasswordResetToken, error) {
	tokenHash := hashToken(token)
	var prt PasswordResetToken
	err := config.DB.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash).Scan(&prt.ID, &prt.UserID, &prt.TokenHash, &prt.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}
	return &prt, nil
}

func UpdateUserPassword(ctx context.Context, userID int, newPasswordHash string) error {
	_, err := config.DB.Exec(ctx,
		`UPDATE users SET password = $1 WHERE id = $2`,
		newPasswordHash, userID)
	return err
}

func UpdateUserInfo(ctx context.Context, userID int, name, email string) error {
	_, err := config.DB.Exec(ctx,
		`UPDATE users SET name = $1, email = $2 WHERE id = $3`,
		name, email, userID)
	return err
}

func DeletePasswordResetToken(ctx context.Context, token string) error {
	tokenHash := hashToken(token)
	_, err := config.DB.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash)
	return err
}

// CreatePasswordResetTable creates the password_reset_tokens table if it doesn't exist.
func CreatePasswordResetTable(ctx context.Context) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// I've added ON DELETE CASCADE to automatically remove reset tokens if a user is deleted.
	// I also added a UNIQUE constraint on user_id to ensure a user only has one active token,
	// and used ON CONFLICT to update it if a new one is requested.
	_, err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create password_reset_tokens table: %w", err)
	}
	return nil
}

// func NewJournal(ctx context.Context, title string, desc string) error {

// 	if config.DB == nil {
// 		return fmt.Errorf("database not initialized")
// 	}

// 	_, err := config.DB.Exec(
// 		ctx,
// 		`INSERT INTO journal ( title, description)
// 		 VALUES ($1, $2)`,
// 		title,
// 		desc,
// 	)

// 	return err
// }

// --- Interests & Onboarding Logic ---

type Interest struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// GetAllInterests retrieves all available interests organized for the UI.
func GetAllInterests(ctx context.Context) ([]Interest, error) {
	if config.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := config.DB.Query(ctx, "SELECT id, name, category FROM interests WHERE is_active = true ORDER BY category, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []Interest
	for rows.Next() {
		var i Interest
		if err := rows.Scan(&i.ID, &i.Name, &i.Category); err != nil {
			return nil, err
		}
		interests = append(interests, i)
	}
	return interests, nil
}

// GetUserInterests retrieves the names of interests selected by a user.
func GetUserInterests(ctx context.Context, userID int) ([]string, error) {
	if config.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := config.DB.Query(ctx, "SELECT i.name FROM interests i JOIN user_interests ui ON i.id = ui.interest_id WHERE ui.user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interests []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		interests = append(interests, name)
	}
	return interests, nil
}

// SaveUserInterests saves the selected interests for a user.
// It clears existing interests first to allow for updates.
func SaveUserInterests(ctx context.Context, userID int, interestIDs []int) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	tx, err := config.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Clear existing
	_, err = tx.Exec(ctx, "DELETE FROM user_interests WHERE user_id = $1", userID)
	if err != nil {
		return err
	}

	// Insert new
	for _, iID := range interestIDs {
		_, err = tx.Exec(ctx, "INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2)", userID, iID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// SaveUserInterestsByNames saves the selected interests for a user using interest names.
func SaveUserInterestsByNames(ctx context.Context, userID int, interestNames []string) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	tx, err := config.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Clear existing
	_, err = tx.Exec(ctx, "DELETE FROM user_interests WHERE user_id = $1", userID)
	if err != nil {
		return err
	}

	// Insert new
	for _, name := range interestNames {
		var interestID int
		// We ignore errors here (e.g. if name not found) to allow partial saves or avoid crashing on stale frontend data
		if err := tx.QueryRow(ctx, "SELECT id FROM interests WHERE name = $1", name).Scan(&interestID); err == nil {
			if _, err := tx.Exec(ctx, "INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2)", userID, interestID); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

// ShouldShowOnboarding checks if the user should see the interest dialog.
// Criteria: User has NO interests set.
// This ensures users who haven't selected interests see the dialog.
func ShouldShowOnboarding(ctx context.Context, userID int) (bool, error) {
	var hasInterests bool
	err := config.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_interests WHERE user_id = $1)", userID).Scan(&hasInterests)
	if err != nil {
		return false, err
	}
	if hasInterests {
		return false, nil
	}

	return true, nil
}

// GetSuggestedUsers finds other users who share similar interests.
func GetSuggestedUsers(ctx context.Context, userID int, limit int) ([]Userinfo, error) {
	rows, err := config.DB.Query(ctx, `
		SELECT DISTINCT u.id, u.name, u.email
		FROM users u
		JOIN user_interests ui ON u.id = ui.user_id
		WHERE u.id != $1
		AND ui.interest_id IN (SELECT interest_id FROM user_interests WHERE user_id = $1)
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []Userinfo
	for rows.Next() {
		var u Userinfo
		// Note: Password is not selected, so it will be empty (safe)
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// SeedInterests populates the interests table with predefined categories and options if not already present.
func SeedInterests(ctx context.Context) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// Ensure tables exist
	_, err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS interests (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			category TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE
		);
		CREATE TABLE IF NOT EXISTS user_interests (
			user_id INT NOT NULL,
			interest_id INT NOT NULL,
			PRIMARY KEY (user_id, interest_id)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create interests tables: %w", err)
	}

	interests := []struct {
		name     string
		category string
	}{ // 🧠 How You’ve Been Feeling
		{"Anxiety", "🧠 How You’ve Been Feeling"},
		{"Overthinking", "🧠 How You’ve Been Feeling"},
		{"Stress", "🧠 How You’ve Been Feeling"},
		{"Loneliness", "🧠 How You’ve Been Feeling"},
		{"Emotional exhaustion", "🧠 How You’ve Been Feeling"},
		{"Calm & clarity", "🧠 How You’ve Been Feeling"},
		{"Gratitude", "🧠 How You’ve Been Feeling"},

		// 🎯 What You’re Working On
		{"Self-discipline", "🎯 What You’re Working On"},
		{"Staying consistent", "🎯 What You’re Working On"},
		{"Finding motivation", "🎯 What You’re Working On"},
		{"Breaking a habit", "🎯 What You’re Working On"},
		{"Improving focus", "🎯 What You’re Working On"},
		{"Building confidence", "🎯 What You’re Working On"},

		// 🧍 Life Situations
		{"Student life", "🧍 Life Situations"},
		{"Career confusion", "🧍 Life Situations"},
		{"Relationship struggles", "🧍 Life Situations"},
		{"Family pressure", "🧍 Life Situations"},
		{"Living alone", "🧍 Life Situations"},
		{"Feeling stuck", "🧍 Life Situations"},

		// 🌱 Reflection & Meaning
		{"Self-reflection", "🌱 Reflection & Meaning"},
		{"Finding purpose", "🌱 Reflection & Meaning"},
		{"Letting go", "🌱 Reflection & Meaning"},
		{"Acceptance", "🌱 Reflection & Meaning"},
		{"Mindfulness", "🌱 Reflection & Meaning"},
		{"Understanding myself better", "🌱 Reflection & Meaning"},

		// 🌙 Time & Energy States
		{"Late-night thoughts", "🌙 Time & Energy States"},
		{"Low-energy days", "🌙 Time & Energy States"},
		{"Need encouragement", "🌙 Time & Energy States"},
		{"Quiet reflection", "🌙 Time & Energy States"},
		{"Morning motivation", "🌙 Time & Energy States"},
	}

	tx, err := config.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, interest := range interests {
		// Check if interest already exists
		var exists bool
		err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM interests WHERE name = $1)", interest.name).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			_, err = tx.Exec(ctx, "INSERT INTO interests (name, category, is_active) VALUES ($1, $2, true)", interest.name, interest.category)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}
