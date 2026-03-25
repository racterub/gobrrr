// Package vault provides AES-256-GCM encryption for secrets at rest.
// The nonce (12 bytes) is randomly generated per encryption and prepended
// to the ciphertext. GCM provides authenticated encryption, so any tampering
// is detected on decryption.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	nonceSize = 12 // GCM standard nonce size in bytes
	keyFile   = "master.key"
	envVar    = "GOBRRR_MASTER_KEY"
)

// Vault encrypts and decrypts secrets using AES-256-GCM.
type Vault struct {
	key [32]byte
}

// GenerateKey generates a cryptographically random 32-byte (256-bit) key.
func GenerateKey() [32]byte {
	var key [32]byte
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		panic(fmt.Sprintf("vault: failed to generate key: %v", err))
	}
	return key
}

// New creates a Vault using the provided key.
func New(key [32]byte) *Vault {
	return &Vault{key: key}
}

// Encrypt encrypts plaintext using AES-256-GCM. The returned bytes are
// [nonce (12 bytes) | ciphertext+tag].
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return nil, fmt.Errorf("vault: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("vault: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Prepend nonce to ciphertext
	result := make([]byte, nonceSize+len(ciphertext))
	copy(result[:nonceSize], nonce)
	copy(result[nonceSize:], ciphertext)
	return result, nil
}

// Decrypt decrypts a ciphertext produced by Encrypt. Returns an error if
// the ciphertext is too short or if GCM authentication fails (tampered data
// or wrong key).
func (v *Vault) Decrypt(data []byte) ([]byte, error) {
	if len(data) < nonceSize {
		return nil, errors.New("vault: ciphertext too short")
	}

	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return nil, fmt.Errorf("vault: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: create GCM: %w", err)
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt: %w", err)
	}
	return plaintext, nil
}

// SaveMasterKey writes key to dir/master.key with 0600 permissions.
func SaveMasterKey(dir string, key [32]byte) error {
	path := filepath.Join(dir, keyFile)
	if err := os.WriteFile(path, key[:], 0600); err != nil {
		return fmt.Errorf("vault: save master key: %w", err)
	}
	return nil
}

// LoadMasterKey loads the master key. It first checks the GOBRRR_MASTER_KEY
// environment variable (hex-encoded). If absent, it reads dir/master.key.
func LoadMasterKey(dir string) ([32]byte, error) {
	var key [32]byte

	if envVal := os.Getenv(envVar); envVal != "" {
		b, err := hex.DecodeString(envVal)
		if err != nil {
			return key, fmt.Errorf("vault: decode %s: %w", envVar, err)
		}
		if len(b) != 32 {
			return key, fmt.Errorf("vault: %s must be 32 bytes (64 hex chars), got %d bytes", envVar, len(b))
		}
		copy(key[:], b)
		return key, nil
	}

	path := filepath.Join(dir, keyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return key, fmt.Errorf("vault: read master key file: %w", err)
	}
	if len(data) != 32 {
		return key, fmt.Errorf("vault: master key file must be 32 bytes, got %d", len(data))
	}
	copy(key[:], data)
	return key, nil
}
