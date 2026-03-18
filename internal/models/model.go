package models

import "time"

type Message struct {
	ID         uint
	SenderID   string `json:\"senderID,string\"`
	SenderName string
	ReceiverID string `json:\"receiverID,string\"`
	Content    string
	IsGroup    bool
	CreatedAt  time.Time
}
