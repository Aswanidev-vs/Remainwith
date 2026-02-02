package models

import "time"

type Message struct {
	ID         uint
	SenderID   string
	SenderName string
	ReceiverID string
	Content    string
	IsGroup    bool
	CreatedAt  time.Time
}
