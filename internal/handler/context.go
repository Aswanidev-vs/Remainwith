package handler

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey contextKey = "user"

func contextWithUser(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, userContextKey, claims)
}

func UserFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(userContextKey).(jwt.MapClaims)
	return claims, ok
}
