package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	tokenKindAccess  = "access"
	tokenKindRefresh = "refresh"
)

// ErrInvalidPassphrase reports a failed login passphrase check.
var ErrInvalidPassphrase = errors.New("invalid passphrase")

// Service signs, verifies, refreshes, and revokes daemon tokens.
type Service struct {
	mu             sync.RWMutex
	secret         []byte
	passphraseHash []byte
	store          *TokenStore
	now            func() time.Time
}

// NewService constructs an authentication service.
func NewService(secret []byte, passphraseHash []byte, store *TokenStore, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		secret:         append([]byte(nil), secret...),
		passphraseHash: append([]byte(nil), passphraseHash...),
		store:          store,
		now:            now,
	}
}

// GenerateSecret creates a 256-bit random JWT secret.
func GenerateSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate jwt secret: %w", err)
	}
	return secret, nil
}

// Login verifies the passphrase hash and returns a token pair.
func (s *Service) Login(ctx context.Context, passphraseHash []byte, clientLabel string) (TokenPair, error) {
	if subtle.ConstantTimeCompare(passphraseHash, s.passphraseHash) != 1 {
		return TokenPair{}, ErrInvalidPassphrase
	}
	return s.issuePair(ctx, clientLabel)
}

// Refresh rotates a refresh token into a new token pair.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	s.mu.RLock()
	secret := append([]byte(nil), s.secret...)
	s.mu.RUnlock()
	claims, err := VerifyToken(secret, refreshToken, s.now())
	if err != nil {
		return TokenPair{}, err
	}
	if claims.Kind != tokenKindRefresh {
		return TokenPair{}, ErrInvalidToken
	}
	if err := s.store.EnsureActive(ctx, claims.ID, s.now()); err != nil {
		return TokenPair{}, err
	}
	if err := s.store.Revoke(ctx, claims.ID); err != nil {
		return TokenPair{}, err
	}
	return s.issuePair(ctx, claims.ClientLabel)
}

// VerifyAccess validates an access token and checks the token repository.
func (s *Service) VerifyAccess(ctx context.Context, accessToken string) (*Claims, error) {
	s.mu.RLock()
	secret := append([]byte(nil), s.secret...)
	s.mu.RUnlock()
	claims, err := VerifyToken(secret, accessToken, s.now())
	if err != nil {
		return nil, err
	}
	if claims.Kind != tokenKindAccess {
		return nil, ErrInvalidToken
	}
	if err := s.store.EnsureActive(ctx, claims.ID, s.now()); err != nil {
		return nil, err
	}
	return claims, nil
}

// Logout revokes all tokens for the provided client label.
func (s *Service) Logout(ctx context.Context, clientLabel string) error {
	return s.store.RevokeByClient(ctx, clientLabel)
}

// RevokeAll invalidates every active token in the store.
func (s *Service) RevokeAll(ctx context.Context) error {
	return s.store.RevokeAll(ctx)
}

// RotateSecret replaces the JWT signing secret and revokes all existing tokens.
// Callers must supply at least 32 bytes of new secret material.
func (s *Service) RotateSecret(ctx context.Context, newSecret []byte) error {
	if len(newSecret) < 32 {
		return fmt.Errorf("new secret too short: need ≥32 bytes")
	}
	if err := s.store.RevokeAll(ctx); err != nil {
		return fmt.Errorf("revoke tokens before rotate: %w", err)
	}
	s.mu.Lock()
	s.secret = append([]byte(nil), newSecret...)
	s.mu.Unlock()
	return nil
}

func (s *Service) issuePair(ctx context.Context, clientLabel string) (TokenPair, error) {
	s.mu.RLock()
	secret := append([]byte(nil), s.secret...)
	s.mu.RUnlock()
	now := s.now().UTC()
	access, accessJTI, err := SignToken(secret, tokenKindAccess, clientLabel, AccessTokenTTL, now)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, refreshJTI, err := SignToken(secret, tokenKindRefresh, clientLabel, RefreshTokenTTL, now)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.store.Save(ctx, accessJTI, tokenKindAccess, clientLabel, now, now.Add(AccessTokenTTL)); err != nil {
		return TokenPair{}, err
	}
	if err := s.store.Save(ctx, refreshJTI, tokenKindRefresh, clientLabel, now, now.Add(RefreshTokenTTL)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// TokenPair is returned to local clients after login or refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}
