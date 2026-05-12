package keyring

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestUnlockCacheExpires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	cache := NewUnlockCache(func() time.Time { return now })

	cache.Set(context.Background(), []byte("secret"), time.Minute)
	got, ok := cache.Get(context.Background())
	if !ok || !bytes.Equal(got, []byte("secret")) {
		t.Fatalf("expected cached key, got %q ok=%v", got, ok)
	}

	now = now.Add(2 * time.Minute)
	_, ok = cache.Get(context.Background())
	if ok {
		t.Fatal("expected cache to expire")
	}
}
