package handler

import (
	"log"
	"os"
)

var JWTKey []byte

func InitJWT() {
	key := os.Getenv("JWTKEY")
	if key == "" {
		log.Fatal("JWTKEY is not set")
	}
	JWTKey = []byte(key)
}
