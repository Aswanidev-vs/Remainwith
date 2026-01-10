package ws

import (
	"encoding/json"
	"time"

	"Remainwith/internal/models"
)

// MessageHandler provides utilities for processing websocket messages
type MessageHandler struct{}

// NewMessageHandler creates a new message handler
func NewMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

// ValidateMessage validates a message before broadcasting
func (mh *MessageHandler) ValidateMessage(msg *models.Message) error {
	if msg.SenderID == "" {
		return ErrEmptySenderID
	}
	if msg.Content == "" {
		return ErrEmptyContent
	}
	if len(msg.Content) > 1000 { // Max message length
		return ErrMessageTooLong
	}
	return nil
}

// PrepareMessage prepares a message for broadcasting
func (mh *MessageHandler) PrepareMessage(senderID, receiverID, content string) models.Message {
	return models.Message{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Content:    content,
		CreatedAt:  time.Now(),
	}
}

// SerializeMessage converts a message to JSON bytes
func (mh *MessageHandler) SerializeMessage(msg models.Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DeserializeMessage converts JSON bytes to a message
func (mh *MessageHandler) DeserializeMessage(data []byte) (models.Message, error) {
	var msg models.Message
	err := json.Unmarshal(data, &msg)
	return msg, err
}

// Error types for message validation
var (
	ErrEmptySenderID  = &MessageError{"sender ID cannot be empty"}
	ErrEmptyContent   = &MessageError{"message content cannot be empty"}
	ErrMessageTooLong = &MessageError{"message content is too long"}
)

// MessageError represents a message validation error
type MessageError struct {
	Message string
}

func (e *MessageError) Error() string {
	return e.Message
}
