package message

import (
	"time"
)

// Message represents a chat message stored in database
type Message struct {
	ID         int64     `json:"id"`
	SenderID   int       `json:"senderID"`
	ReceiverID string    `json:"receiverID"`        // "all", "group_X", or userID string
	GroupID    *int      `json:"groupID,omitempty"` // nil for 1:1/campfire
	Content    string    `json:"content,omitempty"`
	MediaType  string    `json:"mediaType,omitempty"` // "text", "image", "audio", "video"
	MediaURL   string    `json:"mediaURL,omitempty"`
	FileName   string    `json:"fileName,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}
