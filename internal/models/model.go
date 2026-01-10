package models

import "time"

type Message struct {
	ID         uint
	SenderID   string
	ReceiverID string
	Content    string
	CreatedAt  time.Time
}
