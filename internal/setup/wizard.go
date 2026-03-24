// Package setup implements the interactive first-run setup wizard for gobrrr.
package setup

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/racterub/gobrrr/internal/config"
	vault "github.com/racterub/gobrrr/internal/crypto"
	"github.com/racterub/gobrrr/internal/google"
	"github.com/racterub/gobrrr/internal/identity"
)

// RunWizard runs the interactive first-time setup wizard.
// It creates all required directories, generates the master key,
// collects configuration from the user, and optionally installs
// a systemd user unit.
func RunWizard() error {
	reader := bufio.NewReader(os.Stdin)
	gobrrDir := config.GobrrDir()

	fmt.Println("=== gobrrr Setup Wizard ===")
	fmt.Println()

	// 1. Create directory structure.
	fmt.Printf("Creating %s...\n", gobrrDir)
	for _, sub := range []string{"", "logs", "workspace", "workers", "output", "memory", "google"} {
		dir := filepath.Join(gobrrDir, sub)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// 2. Generate or load master key.
	key, err := vault.LoadMasterKey(gobrrDir)
	if err != nil {
		fmt.Println("Generating master key...")
		key = vault.GenerateKey()
		if saveErr := vault.SaveMasterKey(gobrrDir, key); saveErr != nil {
			return fmt.Errorf("saving master key: %w", saveErr)
		}
	} else {
		fmt.Println("Master key already exists.")
	}
	v := vault.New(key)

	// 3. Build config from defaults then prompt for overrides.
	cfg := config.Default()

	fmt.Printf("Max concurrent workers [%d]: ", cfg.MaxWorkers)
	if input := readLine(reader); input != "" {
		if n, parseErr := strconv.Atoi(input); parseErr == nil {
			cfg.MaxWorkers = n
		}
	}

	// 4. Telegram configuration.
	fmt.Println()
	fmt.Println("--- Telegram Configuration ---")
	fmt.Print("Telegram bot token (leave empty to skip): ")
	botToken := readLine(reader)
	if botToken != "" {
		fmt.Print("Telegram chat ID: ")
		chatID := readLine(reader)

		encToken, encErr := v.Encrypt([]byte(botToken))
		if encErr != nil {
			return fmt.Errorf("encrypting bot token: %w", encErr)
		}
		encChatID, encErr := v.Encrypt([]byte(chatID))
		if encErr != nil {
			return fmt.Errorf("encrypting chat ID: %w", encErr)
		}
		cfg.Telegram.BotToken = hex.EncodeToString(encToken)
		cfg.Telegram.ChatID = hex.EncodeToString(encChatID)
	}

	// 5. Uptime Kuma configuration.
	fmt.Println()
	fmt.Println("--- Uptime Kuma (optional) ---")
	fmt.Print("Push URL (leave empty to skip): ")
	pushURL := readLine(reader)
	if pushURL != "" {
		cfg.UptimeKuma.PushURL = pushURL
	}

	// 6. Write config.json.
	configPath := filepath.Join(gobrrDir, "config.json")
	if writeErr := writeJSON(configPath, cfg); writeErr != nil {
		return fmt.Errorf("writing config: %w", writeErr)
	}
	fmt.Printf("Config written to %s\n", configPath)

	// 7. Copy default identity.md if absent.
	identityPath := filepath.Join(gobrrDir, "identity.md")
	if _, statErr := os.Stat(identityPath); os.IsNotExist(statErr) {
		defaultContent, loadErr := identity.Load(gobrrDir)
		if loadErr != nil {
			return fmt.Errorf("loading default identity: %w", loadErr)
		}
		if writeErr := os.WriteFile(identityPath, []byte(defaultContent), 0644); writeErr != nil {
			return fmt.Errorf("writing identity.md: %w", writeErr)
		}
		fmt.Println("Created default identity.md")
	}

	// 8. Google account setup loop.
	fmt.Println()
	fmt.Println("--- Google Account Setup ---")
	for {
		fmt.Print("Add a Google account? [y/N]: ")
		if strings.ToLower(readLine(reader)) != "y" {
			break
		}
		if addErr := setupGoogleAccount(reader, gobrrDir, v); addErr != nil {
			fmt.Printf("Error adding account: %v\n", addErr)
		}
	}

	// 9. Check for agent-browser.
	fmt.Println()
	fmt.Println("--- Browser (agent-browser) ---")
	if _, lookErr := exec.LookPath("agent-browser"); lookErr != nil {
		fmt.Println("agent-browser not found. Install it for web browsing support:")
		fmt.Println("  npm install -g @anthropic-ai/agent-browser")
		fmt.Println("  agent-browser install --with-deps")
	} else {
		fmt.Println("agent-browser found.")
	}

	// 10. Optionally install systemd unit.
	fmt.Println()
	fmt.Print("Install systemd user service? [y/N]: ")
	if strings.ToLower(readLine(reader)) == "y" {
		if svcErr := installSystemdUnit(gobrrDir); svcErr != nil {
			fmt.Printf("Warning: could not install systemd unit: %v\n", svcErr)
		}
	}

	fmt.Println()
	fmt.Println("=== Setup complete! ===")
	fmt.Println("Start the daemon: gobrrr daemon start")
	fmt.Println("Or via systemd:   systemctl --user start gobrrr")

	return nil
}

// RunGoogleAccountSetup runs only the Google account setup flow.
// name is the account label (e.g. "personal", "work").
func RunGoogleAccountSetup(name string) error {
	gobrrDir := config.GobrrDir()

	key, err := vault.LoadMasterKey(gobrrDir)
	if err != nil {
		return fmt.Errorf("loading master key (run 'gobrrr setup' first): %w", err)
	}
	v := vault.New(key)

	reader := bufio.NewReader(os.Stdin)

	if name != "" {
		// Name supplied via flag — skip the prompt but still complete the flow.
		return completeGoogleAccountSetup(reader, gobrrDir, v, name)
	}

	return setupGoogleAccount(reader, gobrrDir, v)
}

// setupGoogleAccount prompts for an account name then calls completeGoogleAccountSetup.
func setupGoogleAccount(reader *bufio.Reader, gobrrDir string, v *vault.Vault) error {
	fmt.Print("Account name (e.g., personal, work): ")
	name := readLine(reader)
	if name == "" {
		return fmt.Errorf("account name cannot be empty")
	}
	return completeGoogleAccountSetup(reader, gobrrDir, v, name)
}

// completeGoogleAccountSetup collects OAuth credentials, runs the OAuth flow,
// and persists the account.
func completeGoogleAccountSetup(reader *bufio.Reader, gobrrDir string, v *vault.Vault, name string) error {
	am := google.NewAccountManager(filepath.Join(gobrrDir, "google"), v)

	fmt.Print("Google OAuth Client ID: ")
	clientID := readLine(reader)
	fmt.Print("Google OAuth Client Secret: ")
	clientSecret := readLine(reader)

	authURL, err := am.StartOAuthFlow(clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("starting OAuth flow: %w", err)
	}

	fmt.Println()
	fmt.Println("Open this URL in your browser:")
	fmt.Println(authURL)
	fmt.Println()
	fmt.Print("Paste the authorization code: ")
	code := readLine(reader)

	token, err := am.CompleteOAuthFlow(code)
	if err != nil {
		return fmt.Errorf("completing OAuth flow: %w", err)
	}

	fmt.Print("Email address for this account: ")
	email := readLine(reader)

	if err := am.SaveAccount(name, email, token, clientID, clientSecret); err != nil {
		return fmt.Errorf("saving account: %w", err)
	}

	// Set as default if no default exists yet.
	if _, defErr := am.GetDefault(); defErr != nil {
		if setErr := am.SetDefault(name); setErr != nil {
			fmt.Printf("Warning: could not set default account: %v\n", setErr)
		}
	}

	fmt.Printf("Account '%s' added.\n", name)
	return nil
}

// installSystemdUnit copies the bundled gobrrr.service to
// ~/.config/systemd/user/ and enables it.
func installSystemdUnit(gobrrDir string) error {
	// Locate the service file next to the binary or in the repo.
	// We look for it relative to the executable first, then fall back to a
	// hard-coded relative path used during development.
	svcContent := defaultServiceUnit

	// Ensure target directory exists.
	systemdUserDir := filepath.Join(mustHomeDir(), ".config", "systemd", "user")
	if err := os.MkdirAll(systemdUserDir, 0755); err != nil {
		return fmt.Errorf("creating systemd user dir: %w", err)
	}

	destPath := filepath.Join(systemdUserDir, "gobrrr.service")
	if err := os.WriteFile(destPath, []byte(svcContent), 0644); err != nil {
		return fmt.Errorf("writing service file: %w", err)
	}
	fmt.Printf("Service file written to %s\n", destPath)

	// Reload and enable.
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "gobrrr"},
	} {
		out, err := exec.Command("systemctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	fmt.Println("systemd unit enabled. Start with: systemctl --user start gobrrr")
	return nil
}

// mustHomeDir returns the home directory or panics.
func mustHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("cannot determine home directory: %v", err))
	}
	return home
}

// writeJSON marshals v as indented JSON and writes it to path (0600).
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// readLine reads a line from reader, trimming trailing whitespace.
func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// defaultServiceUnit is the embedded systemd service file content.
// It mirrors systemd/gobrrr.service in the repository.
const defaultServiceUnit = `[Unit]
Description=gobrrr Task Dispatch Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=%h/.local/bin/gobrrr daemon start
Restart=on-failure
RestartSec=5
WatchdogSec=60
MemoryMax=512M
KillMode=control-group
TimeoutStopSec=30
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gobrrr

[Install]
WantedBy=default.target
`
