package auth

import (
	"errors"
	"testing"
	"time"
)

func TestSignAndVerifyToken(t *testing.T) {
	t.Parallel()

	secret := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	token, jti, err := SignToken(secret, "access", "cli", AccessTokenTTL, now)
	if err != nil {
		t.Fatalf("SignToken returned error: %v", err)
	}
	if jti == "" {
		t.Fatal("expected non-empty jti")
	}

	claims, err := VerifyToken(secret, token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("VerifyToken returned error: %v", err)
	}
	if claims.ID != jti {
		t.Fatalf("claims ID = %q, want %q", claims.ID, jti)
	}
	if claims.Kind != "access" || claims.ClientLabel != "cli" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestVerifyTokenRejectsExpired(t *testing.T) {
	t.Parallel()

	secret := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	token, _, err := SignToken(secret, "access", "cli", time.Minute, now)
	if err != nil {
		t.Fatalf("SignToken returned error: %v", err)
	}

	_, err = VerifyToken(secret, token, now.Add(2*time.Minute))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
