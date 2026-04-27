package daemon

import (
	"os/exec"

	vault "github.com/racterub/gobrrr/internal/crypto"
)

// loadVaultIfAvailable attempts to load the master key and create a vault.
// Returns nil, nil if no key is available (not configured yet).
func loadVaultIfAvailable(gobrrDir string) (*vault.Vault, error) {
	key, err := vault.LoadMasterKey(gobrrDir)
	if err != nil {
		return nil, nil //nolint:nilerr // key absence is expected when not configured
	}
	return vault.New(key), nil
}

func binOnPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
