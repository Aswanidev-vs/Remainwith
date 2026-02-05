package video

import (
	"context"
	"errors"
	"log"

	"Remainwith/internal/signaling"
)

// CreateRoom creates a new room for video calls
func CreateRoom(ctx context.Context, roomName string, hostID, hostName string) (*signaling.Room, error) {
	rm := signaling.GetRoomManager()
	room := rm.CreateRoom(roomName, hostID, hostName)
	if room == nil {
		return nil, errors.New("failed to create room")
	}
	log.Printf("Created room: %s", room.ID)
	return room, nil
}

// CreateRoomWithID creates a room with a specific ID
func CreateRoomWithID(ctx context.Context, roomID, roomName string, hostID, hostName string) (*signaling.Room, error) {
	rm := signaling.GetRoomManager()
	room := rm.CreateRoomWithID(roomID, roomName, hostID, hostName)
	if room == nil {
		return nil, errors.New("failed to create room")
	}
	log.Printf("Created room with ID: %s", room.ID)
	return room, nil
}

// JoinRoom adds a participant to a room
func JoinRoom(roomName, participantName, participantIdentity string) (string, error) {
	rm := signaling.GetRoomManager()
	room, exists := rm.GetRoomByName(roomName)
	if !exists {
		return "", errors.New("room not found")
	}

	participant := room.AddParticipant(participantIdentity, participantName, signaling.RoleMember)
	if participant == nil {
		return "", errors.New("failed to join room")
	}

	log.Printf("Participant %s joined room %s", participantIdentity, room.ID)
	return room.ID, nil
}

// ListParticipants lists participants in a room
func ListParticipants(ctx context.Context, roomName string) ([]*signaling.Participant, error) {
	rm := signaling.GetRoomManager()
	room, exists := rm.GetRoomByName(roomName)
	if !exists {
		return nil, errors.New("room not found")
	}

	participants := room.GetParticipants()
	var participantList []*signaling.Participant
	for _, p := range participants {
		participantList = append(participantList, p)
	}

	return participantList, nil
}

// DeleteRoom deletes a room
func DeleteRoom(ctx context.Context, roomName string) error {
	rm := signaling.GetRoomManager()
	room, exists := rm.GetRoomByName(roomName)
	if !exists {
		return errors.New("room not found")
	}

	rm.DeleteRoom(room.ID)
	log.Printf("Deleted room: %s", room.ID)
	return nil
}
