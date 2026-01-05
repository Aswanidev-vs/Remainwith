package db

import (
	"Remainwith/config"
	"context"
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
		`INSERT INTO users (name, email, password)
         VALUES ($1, $2, $3)`,
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
