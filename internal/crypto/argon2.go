// Package crypto contains key derivation and encrypted storage helpers.
package crypto

import (
	"errors"

	"golang.org/x/crypto/argon2"
)

const (
	// MasterKeySize is the byte length of the SQLCipher master key.
	MasterKeySize = 32

	// ArgonTime is the Argon2id iteration count.
	ArgonTime uint32 = 3
	// ArgonMemory is the Argon2id memory cost in KiB.
	ArgonMemory uint32 = 64 * 1024
	// ArgonThreads is the Argon2id parallelism parameter.
	ArgonThreads uint8 = 4
)

// ErrInvalidKDFInput reports an empty passphrase or too-short salt.
var ErrInvalidKDFInput = errors.New("invalid kdf input")

// DeriveMasterKey derives a 256-bit master key from a passphrase and salt.
func DeriveMasterKey(passphrase []byte, salt []byte) ([]byte, error) {
	if len(passphrase) == 0 || len(salt) < 16 {
		return nil, ErrInvalidKDFInput
	}

	key := argon2.IDKey(passphrase, salt, ArgonTime, ArgonMemory, ArgonThreads, MasterKeySize)
	return key, nil
}
