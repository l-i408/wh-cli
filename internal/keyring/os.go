package keyring

import (
	"context"
	"errors"
	"fmt"

	oskeyring "github.com/zalando/go-keyring"
)

// OSStore stores secrets in the operating system keyring.
type OSStore struct{}

// NewOSStore constructs the OS-backed keyring store.
func NewOSStore() *OSStore {
	return &OSStore{}
}

// Get reads a secret from the OS keyring.
func (s *OSStore) Get(_ context.Context, account string) ([]byte, error) {
	value, err := oskeyring.Get(ServiceName, account)
	if errors.Is(err, oskeyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read keyring item: %w", err)
	}
	return []byte(value), nil
}

// Set writes a secret to the OS keyring.
func (s *OSStore) Set(_ context.Context, account string, value []byte) error {
	if err := oskeyring.Set(ServiceName, account, string(value)); err != nil {
		return fmt.Errorf("write keyring item: %w", err)
	}
	return nil
}

// Delete removes a secret from the OS keyring.
func (s *OSStore) Delete(_ context.Context, account string) error {
	if err := oskeyring.Delete(ServiceName, account); err != nil && !errors.Is(err, oskeyring.ErrNotFound) {
		return fmt.Errorf("delete keyring item: %w", err)
	}
	return nil
}
