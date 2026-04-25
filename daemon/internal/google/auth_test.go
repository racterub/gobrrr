package google_test

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	vault "github.com/racterub/gobrrr/internal/crypto"
	google "github.com/racterub/gobrrr/internal/google"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func newTestManager(t *testing.T) (*google.AccountManager, string) {
	t.Helper()
	dir := t.TempDir()
	key := vault.GenerateKey()
	v := vault.New(key)
	return google.NewAccountManager(dir, v), dir
}

func TestSaveAndLoadAccount(t *testing.T) {
	am, _ := newTestManager(t)
	token := &oauth2.Token{
		RefreshToken: "refresh-123",
		AccessToken:  "access-456",
		TokenType:    "Bearer",
	}

	err := am.SaveAccount("personal", "me@gmail.com", token, "client-id", "client-secret")
	require.NoError(t, err)

	acct, err := am.LoadAccount("personal")
	require.NoError(t, err)

	assert.Equal(t, "me@gmail.com", acct.Email)
	assert.Equal(t, "refresh-123", acct.Token.RefreshToken)
	assert.Equal(t, "access-456", acct.Token.AccessToken)
	assert.Equal(t, "client-id", acct.ClientID)
	assert.Equal(t, "client-secret", acct.ClientSecret)
}

func TestListAccounts(t *testing.T) {
	am, _ := newTestManager(t)
	token := &oauth2.Token{RefreshToken: "r1", AccessToken: "a1", TokenType: "Bearer"}

	err := am.SaveAccount("personal", "personal@gmail.com", token, "cid1", "cs1")
	require.NoError(t, err)

	token2 := &oauth2.Token{RefreshToken: "r2", AccessToken: "a2", TokenType: "Bearer"}
	err = am.SaveAccount("work", "work@company.com", token2, "cid2", "cs2")
	require.NoError(t, err)

	accounts, err := am.ListAccounts()
	require.NoError(t, err)

	assert.Len(t, accounts, 2)
	assert.Equal(t, "personal@gmail.com", accounts["personal"])
	assert.Equal(t, "work@company.com", accounts["work"])
}

func TestDefaultAccount(t *testing.T) {
	am, _ := newTestManager(t)
	token := &oauth2.Token{RefreshToken: "r", AccessToken: "a", TokenType: "Bearer"}

	err := am.SaveAccount("personal", "personal@gmail.com", token, "cid", "cs")
	require.NoError(t, err)

	err = am.SetDefault("personal")
	require.NoError(t, err)

	def, err := am.GetDefault()
	require.NoError(t, err)
	assert.Equal(t, "personal", def)
}

func TestCredentialsEncrypted(t *testing.T) {
	am, dir := newTestManager(t)
	token := &oauth2.Token{RefreshToken: "secret-refresh", AccessToken: "secret-access", TokenType: "Bearer"}

	err := am.SaveAccount("personal", "me@gmail.com", token, "client-id", "client-secret")
	require.NoError(t, err)

	encFile := filepath.Join(dir, "personal", "credentials.enc")
	raw, err := os.ReadFile(encFile)
	require.NoError(t, err)

	// Verify the raw bytes do not contain plaintext secrets
	assert.False(t, bytes.Contains(raw, []byte("secret-refresh")), "token should not appear in plaintext")
	assert.False(t, bytes.Contains(raw, []byte("client-secret")), "client secret should not appear in plaintext")
	assert.False(t, bytes.Contains(raw, []byte("me@gmail.com")), "email should not appear in plaintext")
}

func TestLoadAccountNotFound(t *testing.T) {
	am, _ := newTestManager(t)

	_, err := am.LoadAccount("nonexistent")
	assert.Error(t, err)
}

func TestSetDefaultNonExistentAccount(t *testing.T) {
	am, _ := newTestManager(t)

	err := am.SetDefault("ghost")
	assert.Error(t, err)
}

func TestGetDefaultNoDefault(t *testing.T) {
	am, _ := newTestManager(t)

	_, err := am.GetDefault()
	assert.Error(t, err)
}

func TestGetHTTPClient(t *testing.T) {
	am, _ := newTestManager(t)
	token := &oauth2.Token{RefreshToken: "r", AccessToken: "a", TokenType: "Bearer"}

	err := am.SaveAccount("personal", "me@gmail.com", token, "client-id", "client-secret")
	require.NoError(t, err)

	client, err := am.GetHTTPClient("personal")
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestSaveAccount_RewriteUsesAtomicRename(t *testing.T) {
	// os.WriteFile truncates in place — same inode after rewrite.
	// Atomic .tmp + rename produces a new inode. Rewriting the same
	// account exercises both files: credentials.enc (per-account blob)
	// and accounts.json (shared index). Both must be atomic so a crash
	// mid-write can't surface a half-written file to readers.
	am, dir := newTestManager(t)
	token := &oauth2.Token{RefreshToken: "r", AccessToken: "a", TokenType: "Bearer"}

	require.NoError(t, am.SaveAccount("alice", "a@example.com", token, "cid", "cs"))
	idxPath := filepath.Join(dir, "accounts.json")
	encPath := filepath.Join(dir, "alice", "credentials.enc")
	idxInodeBefore := inodeOf(t, idxPath)
	encInodeBefore := inodeOf(t, encPath)

	require.NoError(t, am.SaveAccount("alice", "a@example.com", token, "cid", "cs2"))
	assert.NotEqual(t, idxInodeBefore, inodeOf(t, idxPath),
		"accounts.json rewrite must use atomic .tmp + rename, not in-place truncate")
	assert.NotEqual(t, encInodeBefore, inodeOf(t, encPath),
		"credentials.enc rewrite must use atomic .tmp + rename, not in-place truncate")
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestStartAndCompleteOAuthFlow(t *testing.T) {
	am, _ := newTestManager(t)

	authURL, err := am.StartOAuthFlow("my-client-id", "my-client-secret")
	require.NoError(t, err)
	assert.Contains(t, authURL, "accounts.google.com")
	assert.Contains(t, authURL, "my-client-id")
}
