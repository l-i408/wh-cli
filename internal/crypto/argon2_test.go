package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestDeriveMasterKey(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")
	key, err := DeriveMasterKey([]byte("correct horse battery staple"), salt)
	if err != nil {
		t.Fatalf("DeriveMasterKey returned error: %v", err)
	}
	if len(key) != MasterKeySize {
		t.Fatalf("key length = %d, want %d", len(key), MasterKeySize)
	}

	again, err := DeriveMasterKey([]byte("correct horse battery staple"), salt)
	if err != nil {
		t.Fatalf("DeriveMasterKey returned error: %v", err)
	}
	if !bytes.Equal(key, again) {
		t.Fatal("expected deterministic key for same passphrase and salt")
	}
}

func TestDeriveMasterKeyRejectsWeakInput(t *testing.T) {
	t.Parallel()

	_, err := DeriveMasterKey(nil, []byte("0123456789abcdef"))
	if !errors.Is(err, ErrInvalidKDFInput) {
		t.Fatalf("expected ErrInvalidKDFInput for empty passphrase, got %v", err)
	}

	_, err = DeriveMasterKey([]byte("passphrase"), []byte("short"))
	if !errors.Is(err, ErrInvalidKDFInput) {
		t.Fatalf("expected ErrInvalidKDFInput for short salt, got %v", err)
	}
}
