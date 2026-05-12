package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/l-i408/wh-cli/internal/store"
)

func newTestService(t *testing.T, now *time.Time) *Service {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return NewService(
		[]byte("01234567890123456789012345678901"),
		[]byte("pass-hash"),
		NewTokenStore(db),
		func() time.Time { return *now },
	)
}

func TestServiceLoginVerifyRefreshAndLogout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, &now)

	pair, err := svc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected token pair")
	}

	claims, err := svc.VerifyAccess(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess returned error: %v", err)
	}
	if claims.ClientLabel != "cli" {
		t.Fatalf("client label = %q, want cli", claims.ClientLabel)
	}

	rotated, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}

	if _, err := svc.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("expected revoked old refresh token, got %v", err)
	}

	if err := svc.Logout(context.Background(), "cli"); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if _, err := svc.VerifyAccess(context.Background(), rotated.AccessToken); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("expected revoked access token, got %v", err)
	}
}

func TestServiceLoginRejectsWrongPassphrase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, &now)

	_, err := svc.Login(context.Background(), []byte("wrong"), "cli")
	if !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}
