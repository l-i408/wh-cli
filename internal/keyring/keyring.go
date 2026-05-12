// Package keyring defines OS keyring access and temporary unlock state.
package keyring

import (
	"context"
	"errors"
)

const (
	// ServiceName is the OS keyring service namespace.
	ServiceName = "wh-cli"
	// AccountMasterKey stores the SQLCipher master key.
	AccountMasterKey = "master-key"
	// AccountJWTSecret stores the JWT signing secret.
	AccountJWTSecret = "jwt-secret"
	// AccountAccessToken stores the CLI access token.
	AccountAccessToken = "cli-access-token"
	// AccountRefreshToken is the keyring account name for the CLI refresh token.
	// #nosec G101 -- this is a public keyring account label, not a token value.
	AccountRefreshToken = "cli-refresh-token"
	// AccountLocalPassphraseHash stores the local bootstrap auth secret hash.
	AccountLocalPassphraseHash = "local-passphrase-hash"
)

// ErrNotFound reports a missing keyring item.
var ErrNotFound = errors.New("keyring item not found")

// Store abstracts the OS keyring for secrets used by the daemon.
type Store interface {
	Get(ctx context.Context, account string) ([]byte, error)
	Set(ctx context.Context, account string, value []byte) error
	Delete(ctx context.Context, account string) error
}
