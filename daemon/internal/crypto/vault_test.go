package vault_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	vault "github.com/racterub/gobrrr/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKey(t *testing.T) {
	key := vault.GenerateKey()
	assert.Len(t, key, 32, "master key must be 32 bytes")

	// Two generated keys should differ
	key2 := vault.GenerateKey()
	assert.NotEqual(t, key, key2, "two generated keys should not be equal")
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := vault.GenerateKey()
	v := vault.New(key)
	plaintext := "my-secret-token"
	encrypted, err := v.Encrypt([]byte(plaintext))
	require.NoError(t, err)
	decrypted, err := v.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, string(decrypted))
}

func TestEncryptProducesNonDeterministicOutput(t *testing.T) {
	key := vault.GenerateKey()
	v := vault.New(key)
	plaintext := []byte("hello")

	enc1, err := v.Encrypt(plaintext)
	require.NoError(t, err)
	enc2, err := v.Encrypt(plaintext)
	require.NoError(t, err)

	// Each encryption uses a random nonce, so ciphertexts must differ
	assert.NotEqual(t, enc1, enc2)
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := vault.GenerateKey()
	key2 := vault.GenerateKey()
	v1 := vault.New(key1)
	v2 := vault.New(key2)
	encrypted, err := v1.Encrypt([]byte("secret"))
	require.NoError(t, err)
	_, err = v2.Decrypt(encrypted)
	assert.Error(t, err)
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key := vault.GenerateKey()
	v := vault.New(key)
	encrypted, err := v.Encrypt([]byte("tamper-me"))
	require.NoError(t, err)

	// Flip a byte in the ciphertext portion (after the 12-byte nonce)
	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	tampered[12] ^= 0xFF

	_, err = v.Decrypt(tampered)
	assert.Error(t, err, "GCM integrity check should fail on tampered ciphertext")
}

func TestDecryptTooShort(t *testing.T) {
	key := vault.GenerateKey()
	v := vault.New(key)

	// Anything shorter than nonce size should fail
	_, err := v.Decrypt([]byte("short"))
	assert.Error(t, err)
}

func TestSaveAndLoadMasterKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	key := vault.GenerateKey()

	err := vault.SaveMasterKey(dir, key)
	require.NoError(t, err)

	// Check file permissions
	info, err := os.Stat(filepath.Join(dir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	loaded, err := vault.LoadMasterKey(dir)
	require.NoError(t, err)
	assert.Equal(t, key, loaded)
}

func TestLoadMasterKeyFromEnvVar(t *testing.T) {
	dir := t.TempDir()
	key := vault.GenerateKey()
	t.Setenv("GOBRRR_MASTER_KEY", hex.EncodeToString(key[:]))

	loaded, err := vault.LoadMasterKey(dir)
	require.NoError(t, err)
	assert.Equal(t, key, loaded)
}

func TestEnvVarTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()

	fileKey := vault.GenerateKey()
	require.NoError(t, vault.SaveMasterKey(dir, fileKey))

	envKey := vault.GenerateKey()
	t.Setenv("GOBRRR_MASTER_KEY", hex.EncodeToString(envKey[:]))

	loaded, err := vault.LoadMasterKey(dir)
	require.NoError(t, err)
	assert.Equal(t, envKey, loaded, "env var key should take precedence over file key")
	assert.NotEqual(t, fileKey, loaded)
}

func TestLoadMasterKeyInvalidHexEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOBRRR_MASTER_KEY", "not-valid-hex!!!")

	_, err := vault.LoadMasterKey(dir)
	assert.Error(t, err)
}

func TestLoadMasterKeyEnvVarWrongLength(t *testing.T) {
	dir := t.TempDir()
	// Valid hex but only 16 bytes (not 32)
	t.Setenv("GOBRRR_MASTER_KEY", hex.EncodeToString(make([]byte, 16)))

	_, err := vault.LoadMasterKey(dir)
	assert.Error(t, err)
}

func TestLoadMasterKeyMissingFileAndNoEnvVar(t *testing.T) {
	dir := t.TempDir()
	// No env var, no file
	_, err := vault.LoadMasterKey(dir)
	assert.Error(t, err)
}
