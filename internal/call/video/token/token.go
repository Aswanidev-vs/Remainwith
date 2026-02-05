package token

import (
	"errors"
	"os"
	"time"

	"github.com/livekit/protocol/auth"
)

func GenerateAccessToken(roomName, participantName, participantIdentity string) (string, error) {
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		return "", errors.New("LIVEKIT_API_KEY or LIVEKIT_API_SECRET is not set in the environment")
	}

	at := auth.NewAccessToken(apiKey, apiSecret)
	grant := &auth.VideoGrant{
		RoomJoin: true,
		Room:     roomName,
	}
	at.AddGrant(grant).
		SetIdentity(participantIdentity).
		SetName(participantName).
		SetValidFor(time.Hour)

	token, err := at.ToJWT()
	if err != nil {
		return "", err
	}
	return token, nil
}
