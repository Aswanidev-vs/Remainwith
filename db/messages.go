package db

import (
	"Remainwith/config"
	"context"
	"fmt"
	"strconv"
	"time"
)

// Message represents a chat message stored in database
type Message struct {
	ID         int64     `json:"id"`
	SenderID   int       `json:"senderID"`
	SenderName string    `json:"senderName,omitempty"`
	ReceiverID string    `json:"receiverID"`
	GroupID    *int      `json:"groupID,omitempty"`
	Content    string    `json:"content,omitempty"`
	MediaType  string    `json:"mediaType,omitempty"`
	MediaURL   string    `json:"mediaURL,omitempty"`
	FileName   string    `json:"fileName,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// InitMessageTable creates the messages table if it doesn't exist.
func InitMessageTable(ctx context.Context) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	// Ensure group tables are also initialized
	if err := InitGroupTables(ctx); err != nil {
		return err
	}
	_, err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			sender_id INT NOT NULL,
			receiver_id TEXT NOT NULL,
			group_id INT,
			content TEXT,
			media_type TEXT,
			media_url TEXT,
			file_name TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_messages_group_id ON messages(group_id);
		CREATE INDEX IF NOT EXISTS idx_messages_receiver_id ON messages(receiver_id);
		CREATE INDEX IF NOT EXISTS idx_messages_sender_id ON messages(sender_id);
		CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
		CREATE INDEX IF NOT EXISTS idx_messages_group_id_id_desc ON messages(group_id, id DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_receiver_id_id_desc ON messages(receiver_id, id DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_sender_receiver_id_desc ON messages(sender_id, receiver_id, id DESC);
	`)
	return err
}

// GetMessages retrieves history for a specific context (1:1, Group, or Campfire)
func GetMessages(ctx context.Context, currentUserID int, otherID string, groupID *int, limit int, beforeID *int64) ([]Message, error) {

	if config.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT m.id, m.sender_id, u.name, m.receiver_id, m.group_id,
		       COALESCE(m.content, ''), COALESCE(m.media_type, ''), COALESCE(m.media_url, ''),
		       COALESCE(m.file_name, ''), m.created_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE 1=1
	`
	var args []interface{}
	argCount := 1

	if groupID != nil {
		// Group Chat
		query += fmt.Sprintf(" AND m.group_id = $%d", argCount)
		args = append(args, *groupID)
		argCount++
	} else if otherID == "all" {
		// Public Campfire
		query += " AND m.receiver_id = 'all' AND m.group_id IS NULL"
	} else {
		// 1:1 Chat
		// Fetch messages where (sender=Me AND receiver=Them) OR (sender=Them AND receiver=Me)

		otherIDInt, err := strconv.Atoi(otherID)
		if err != nil {
			return []Message{}, nil // Invalid user ID
		}

		query += fmt.Sprintf(" AND ( (m.sender_id = $%d AND m.receiver_id = $%d) OR (m.sender_id = $%d AND m.receiver_id = $%d) )",
			argCount, argCount+1, argCount+2, argCount+3)

		args = append(args, currentUserID, otherID, otherIDInt, strconv.Itoa(currentUserID))
		argCount += 4
	}

	if beforeID != nil {
		query += fmt.Sprintf(" AND m.id < $%d", argCount)
		args = append(args, *beforeID)
		argCount++
	}

	query += fmt.Sprintf(" ORDER BY m.id DESC LIMIT $%d", argCount)
	args = append(args, limit)

	rows, err := config.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var groupIDNull *int

		err := rows.Scan(
			&m.ID,
			&m.SenderID,
			&m.SenderName,
			&m.ReceiverID,
			&groupIDNull,
			&m.Content,
			&m.MediaType,
			&m.MediaURL,
			&m.FileName,
			&m.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		m.GroupID = groupIDNull
		messages = append(messages, m)
	}

	return messages, nil
}

// SaveMessage saves a new message to the database.
func SaveMessage(ctx context.Context, msg *Message) (int64, error) {
	if config.DB == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	var messageID int64
	err := config.DB.QueryRow(ctx, `
		INSERT INTO messages (sender_id, receiver_id, group_id, content, media_type, media_url, file_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, msg.SenderID, msg.ReceiverID, msg.GroupID, msg.Content, msg.MediaType, msg.MediaURL, msg.FileName).Scan(&messageID)

	return messageID, err
}

// DeleteMessage deletes a message if it belongs to the requesting sender.
func DeleteMessage(ctx context.Context, messageID int64, senderID int) error {
	if config.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	tag, err := config.DB.Exec(ctx, `
		DELETE FROM messages
		WHERE id = $1 AND sender_id = $2
	`, messageID, senderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("message not found or not owned by sender")
	}
	return nil
}
