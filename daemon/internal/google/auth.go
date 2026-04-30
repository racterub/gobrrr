// Package google provides Google OAuth account management with encrypted storage.
// Accounts are stored in a directory with per-account encrypted credential files.
// The master encryption key comes from the crypto vault.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/racterub/gobrrr/internal/atomicfs"
	vault "github.com/racterub/gobrrr/internal/crypto"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

const (
	accountsFile    = "accounts.json"
	credentialsFile = "credentials.enc"
	accountsVersion = 1
)

// oauthScopes are the Google API scopes requested for each account.
var oauthScopes = []string{
	gmail.GmailReadonlyScope,
	gmail.GmailSendScope,
	calendar.CalendarScope,
}

// Account holds the OAuth credentials and metadata for a Google account.
type Account struct {
	Email        string
	Token        *oauth2.Token
	ClientID     string
	ClientSecret string
}

// accountEntry is the per-account record stored in accounts.json.
type accountEntry struct {
	Email string `json:"email"`
}

// accountsIndex is the top-level structure of accounts.json.
type accountsIndex struct {
	Version  int                     `json:"version"`
	Default  string                  `json:"default,omitempty"`
	Accounts map[string]accountEntry `json:"accounts"`
}

// credentialsPayload is the JSON structure written to credentials.enc (before encryption).
type credentialsPayload struct {
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret"`
	Token        *oauth2.Token `json:"token"`
}

// AccountManager manages Google OAuth accounts on disk.
// Credentials are stored encrypted using the provided vault.
type AccountManager struct {
	dir         string
	vault       *vault.Vault
	pendingConf *oauth2.Config // set during StartOAuthFlow, consumed by CompleteOAuthFlow
}

// NewAccountManager creates an AccountManager that stores accounts under dir.
func NewAccountManager(dir string, v *vault.Vault) *AccountManager {
	return &AccountManager{dir: dir, vault: v}
}

// SaveAccount encrypts and persists an account's credentials to disk.
// It writes <dir>/<name>/credentials.enc and updates accounts.json.
func (am *AccountManager) SaveAccount(name, email string, token *oauth2.Token, clientID, clientSecret string) error {
	payload := credentialsPayload{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Token:        token,
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("google: marshal credentials: %w", err)
	}

	ciphertext, err := am.vault.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("google: encrypt credentials: %w", err)
	}

	accountDir := filepath.Join(am.dir, name)
	if err := os.MkdirAll(accountDir, 0700); err != nil {
		return fmt.Errorf("google: create account dir: %w", err)
	}

	encPath := filepath.Join(accountDir, credentialsFile)
	if err := atomicfs.WriteFile(encPath, ciphertext, 0600); err != nil {
		return fmt.Errorf("google: write credentials: %w", err)
	}

	if err := am.updateIndex(name, email); err != nil {
		return fmt.Errorf("google: update index: %w", err)
	}

	return nil
}

// LoadAccount reads and decrypts an account's credentials from disk.
func (am *AccountManager) LoadAccount(name string) (*Account, error) {
	encPath := filepath.Join(am.dir, name, credentialsFile)
	ciphertext, err := os.ReadFile(encPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("google: account %q not found", name)
		}
		return nil, fmt.Errorf("google: read credentials: %w", err)
	}

	plaintext, err := am.vault.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("google: decrypt credentials: %w", err)
	}

	var payload credentialsPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("google: unmarshal credentials: %w", err)
	}

	idx, err := am.loadIndex()
	if err != nil {
		return nil, err
	}

	entry, ok := idx.Accounts[name]
	if !ok {
		return nil, fmt.Errorf("google: account %q not in index", name)
	}

	return &Account{
		Email:        entry.Email,
		Token:        payload.Token,
		ClientID:     payload.ClientID,
		ClientSecret: payload.ClientSecret,
	}, nil
}

// ListAccounts returns a map of account name to email for all saved accounts.
func (am *AccountManager) ListAccounts() (map[string]string, error) {
	idx, err := am.loadIndex()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(idx.Accounts))
	for name, entry := range idx.Accounts {
		result[name] = entry.Email
	}
	return result, nil
}

// GetDefault returns the name of the default account.
// Returns an error if no default has been set.
func (am *AccountManager) GetDefault() (string, error) {
	idx, err := am.loadIndex()
	if err != nil {
		return "", err
	}
	if idx.Default == "" {
		return "", errors.New("google: no default account set")
	}
	return idx.Default, nil
}

// SetDefault sets the named account as the default.
// Returns an error if the account does not exist.
func (am *AccountManager) SetDefault(name string) error {
	idx, err := am.loadIndex()
	if err != nil {
		return err
	}
	if _, ok := idx.Accounts[name]; !ok {
		return fmt.Errorf("google: account %q not found", name)
	}
	idx.Default = name
	return am.saveIndex(idx)
}

// GetHTTPClient returns an *http.Client authenticated as the named account.
// The oauth2 transport handles automatic token refresh.
func (am *AccountManager) GetHTTPClient(name string) (*http.Client, error) {
	acct, err := am.LoadAccount(name)
	if err != nil {
		return nil, err
	}

	conf := &oauth2.Config{
		ClientID:     acct.ClientID,
		ClientSecret: acct.ClientSecret,
		Scopes:       oauthScopes,
		Endpoint:     google.Endpoint,
	}

	return conf.Client(context.Background(), acct.Token), nil
}

// StartOAuthFlow creates an OAuth2 authorization URL for the given client credentials.
// The returned URL should be opened by the user in a browser.
// Call CompleteOAuthFlow with the authorization code once the user has authorized.
func (am *AccountManager) StartOAuthFlow(clientID, clientSecret string) (authURL string, err error) {
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       oauthScopes,
		Endpoint:     google.Endpoint,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
	}
	am.pendingConf = conf
	authURL = conf.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	return authURL, nil
}

// CompleteOAuthFlow exchanges the authorization code for an OAuth2 token.
// StartOAuthFlow must have been called first.
func (am *AccountManager) CompleteOAuthFlow(code string) (*oauth2.Token, error) {
	if am.pendingConf == nil {
		return nil, errors.New("google: no pending OAuth flow; call StartOAuthFlow first")
	}

	token, err := am.pendingConf.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("google: exchange auth code: %w", err)
	}

	am.pendingConf = nil
	return token, nil
}

// loadIndex reads accounts.json. Returns an empty index if the file does not exist.
func (am *AccountManager) loadIndex() (*accountsIndex, error) {
	idxPath := filepath.Join(am.dir, accountsFile)
	data, err := os.ReadFile(idxPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &accountsIndex{
				Version:  accountsVersion,
				Accounts: make(map[string]accountEntry),
			}, nil
		}
		return nil, fmt.Errorf("google: read accounts index: %w", err)
	}

	var idx accountsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("google: unmarshal accounts index: %w", err)
	}
	if idx.Accounts == nil {
		idx.Accounts = make(map[string]accountEntry)
	}
	return &idx, nil
}

// saveIndex writes accounts.json atomically via .tmp + rename.
func (am *AccountManager) saveIndex(idx *accountsIndex) error {
	data, err := json.MarshalIndent(idx, "", "    ")
	if err != nil {
		return fmt.Errorf("google: marshal accounts index: %w", err)
	}
	idxPath := filepath.Join(am.dir, accountsFile)
	if err := atomicfs.WriteFile(idxPath, data, 0600); err != nil {
		return fmt.Errorf("google: write accounts index: %w", err)
	}
	return nil
}

// updateIndex adds or updates the entry for name in accounts.json.
func (am *AccountManager) updateIndex(name, email string) error {
	idx, err := am.loadIndex()
	if err != nil {
		return err
	}
	idx.Accounts[name] = accountEntry{Email: email}
	return am.saveIndex(idx)
}
