// Package setup implements the interactive first-run setup wizard for gobrrr.
package setup

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/racterub/gobrrr/internal/config"
	vault "github.com/racterub/gobrrr/internal/crypto"
	"github.com/racterub/gobrrr/internal/google"
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
	for _, sub := range []string{"", "logs", "workers", "output", "memory", "google"} {
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
	fmt.Println("Example: http://uptime-kuma:3001/api/push/aBcDeFgH")
	fmt.Print("Push URL (leave empty to skip): ")
	pushURL := readLine(reader)
	if pushURL != "" {
		// Strip query parameters — the heartbeat builds its own.
		if u, err := url.Parse(pushURL); err == nil {
			u.RawQuery = ""
			pushURL = u.String()
		}
		cfg.UptimeKuma.PushURL = pushURL
	}

	// 5b. Telegram session configuration.
	fmt.Println()
	fmt.Println("--- Telegram Session ---")
	fmt.Print("Enable Telegram channel session? [y/N]: ")
	if input := readLine(reader); strings.ToLower(input) == "y" {
		cfg.TelegramSession.Enabled = true

		fmt.Printf("Memory ceiling (MB) [%d]: ", cfg.TelegramSession.MemoryCeilingMB)
		if input := readLine(reader); input != "" {
			if v, err := strconv.Atoi(input); err == nil {
				cfg.TelegramSession.MemoryCeilingMB = v
			}
		}

		fmt.Printf("Max uptime (hours) [%d]: ", cfg.TelegramSession.MaxUptimeHours)
		if input := readLine(reader); input != "" {
			if v, err := strconv.Atoi(input); err == nil {
				cfg.TelegramSession.MaxUptimeHours = v
			}
		}

		fmt.Printf("Idle threshold (minutes) [%d]: ", cfg.TelegramSession.IdleThresholdMin)
		if input := readLine(reader); input != "" {
			if v, err := strconv.Atoi(input); err == nil {
				cfg.TelegramSession.IdleThresholdMin = v
			}
		}

		fmt.Print("Channels [plugin:telegram@claude-plugins-official]: ")
		if input := readLine(reader); input != "" {
			cfg.TelegramSession.Channels = []string{input}
		} else {
			cfg.TelegramSession.Channels = []string{"plugin:telegram@claude-plugins-official"}
		}
	}

	// 6. Write config.json.
	configPath := filepath.Join(gobrrDir, "config.json")
	if writeErr := writeJSON(configPath, cfg); writeErr != nil {
		return fmt.Errorf("writing config: %w", writeErr)
	}
	fmt.Printf("Config written to %s\n", configPath)

	// 7. Google account setup loop.
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

	fmt.Println()
	fmt.Println("=== Setup complete! ===")
	fmt.Println("Start the daemon: gobrrr daemon start")
	fmt.Println("Or via systemd:   sudo systemctl start gobrrr")

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

