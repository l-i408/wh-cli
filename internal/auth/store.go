package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/l-i408/wh-cli/internal/store"
)

// ErrTokenRevoked reports a token that exists but is no longer usable.
var ErrTokenRevoked = errors.New("token revoked")

// TokenStore persists JWT IDs for revocation and refresh rotation.
type TokenStore struct {
	db *store.DB
}

// NewTokenStore constructs a token repository.
func NewTokenStore(db *store.DB) *TokenStore {
	return &TokenStore{db: db}
}

// Save records a signed token by jti.
func (s *TokenStore) Save(ctx context.Context, jti string, kind string, clientLabel string, issuedAt time.Time, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO auth_tokens (jti, kind, client_label, issued_at, expires_at, revoked)
VALUES (?, ?, ?, ?, ?, 0)
`, jti, kind, clientLabel, issuedAt.UTC().Format(time.RFC3339), expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("save auth token: %w", err)
	}
	return nil
}

// EnsureActive verifies that a token jti exists and has not been revoked.
func (s *TokenStore) EnsureActive(ctx context.Context, jti string, now time.Time) error {
	var revoked bool
	var expiresRaw string
	err := s.db.QueryRow(ctx, `SELECT revoked, expires_at FROM auth_tokens WHERE jti = ?`, jti).Scan(&revoked, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("read auth token: %w", err)
	}
	if revoked {
		return ErrTokenRevoked
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresRaw)
	if err != nil {
		return fmt.Errorf("parse token expiry: %w", err)
	}
	if !now.Before(expiresAt) {
		return ErrInvalidToken
	}
	return nil
}

// Revoke marks a jti as unusable.
func (s *TokenStore) Revoke(ctx context.Context, jti string) error {
	_, err := s.db.Exec(ctx, `UPDATE auth_tokens SET revoked = 1 WHERE jti = ?`, jti)
	if err != nil {
		return fmt.Errorf("revoke auth token: %w", err)
	}
	return nil
}

// RevokeByClient marks all tokens for a client as unusable.
func (s *TokenStore) RevokeByClient(ctx context.Context, clientLabel string) error {
	_, err := s.db.Exec(ctx, `UPDATE auth_tokens SET revoked = 1 WHERE client_label = ?`, clientLabel)
	if err != nil {
		return fmt.Errorf("revoke auth tokens by client: %w", err)
	}
	return nil
}

// RevokeAll marks every token in the store as unusable.
func (s *TokenStore) RevokeAll(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `UPDATE auth_tokens SET revoked = 1`)
	if err != nil {
		return fmt.Errorf("revoke all auth tokens: %w", err)
	}
	return nil
}
