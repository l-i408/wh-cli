package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	exportMagic   = "WHCL"
	exportVersion = byte(1)
	saltSize      = 32
	nonceSize     = 12
)

// ErrInvalidExport reports a corrupt or unrecognised export file.
var ErrInvalidExport = errors.New("invalid export file")

// EncryptExport encrypts plaintext with a key derived from passphrase.
// Format: magic(4) | version(1) | salt(32) | nonce(12) | ciphertext
func EncryptExport(passphrase []byte, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	key, err := DeriveMasterKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive export key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, 4+1+saltSize+nonceSize+len(ciphertext))
	out = append(out, []byte(exportMagic)...)
	out = append(out, exportVersion)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// DecryptExport decrypts an export produced by EncryptExport.
func DecryptExport(passphrase []byte, data []byte) ([]byte, error) {
	headerSize := len(exportMagic) + 1 + saltSize + nonceSize
	if len(data) < headerSize {
		return nil, ErrInvalidExport
	}
	if string(data[:4]) != exportMagic {
		return nil, ErrInvalidExport
	}
	if data[4] != exportVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidExport, data[4])
	}
	salt := data[5 : 5+saltSize]
	nonce := data[5+saltSize : 5+saltSize+nonceSize]
	ciphertext := data[5+saltSize+nonceSize:]

	key, err := DeriveMasterKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive export key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: decryption failed (wrong passphrase?)", ErrInvalidExport)
	}
	return plaintext, nil
}
