// Package auth implements daemon authentication primitives.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	// AccessTokenTTL is the lifetime for bearer access tokens.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL is the lifetime for refresh tokens.
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// ErrInvalidToken reports malformed, expired, or unverifiable JWTs.
var ErrInvalidToken = errors.New("invalid token")

// Claims is the wh-cli JWT claims payload.
type Claims struct {
	Kind        string `json:"kind"`
	ClientLabel string `json:"client_label"`
	jwt.RegisteredClaims
}

// SignToken creates an HS256 JWT and returns the signed token plus its jti.
func SignToken(secret []byte, kind string, clientLabel string, ttl time.Duration, now time.Time) (string, string, error) {
	if len(secret) < 32 {
		return "", "", ErrInvalidToken
	}

	jti := uuid.NewString()
	claims := Claims{
		Kind:        kind,
		ClientLabel: clientLabel,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "", "", fmt.Errorf("sign jwt: %w", err)
	}
	return token, jti, nil
}

// VerifyToken validates an HS256 JWT and returns its claims.
func VerifyToken(secret []byte, tokenString string, now time.Time) (*Claims, error) {
	claims := &Claims{}
	parser := jwt.NewParser(jwt.WithTimeFunc(func() time.Time { return now }))
	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
