package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// TelegramConfig holds Telegram bot configuration.
// BotToken and ChatID are encrypted references managed by the crypto vault.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// UptimeKumaConfig holds Uptime Kuma heartbeat configuration.
type UptimeKumaConfig struct {
	PushURL     string `json:"push_url"`
	IntervalSec int    `json:"interval_sec"`
}

// Config is the top-level daemon configuration.
type Config struct {
	Version           int              `json:"version"`
	MaxWorkers        int              `json:"max_workers"`
	DefaultTimeoutSec int              `json:"default_timeout_sec"`
	SpawnIntervalSec  int              `json:"spawn_interval_sec"`
	LogRetentionDays  int              `json:"log_retention_days"`
	SocketPath        string           `json:"socket_path"`
	WorkspacePath     string           `json:"workspace_path"`
	Telegram          TelegramConfig   `json:"telegram"`
	UptimeKuma        UptimeKumaConfig `json:"uptime_kuma"`
}

// Default returns a Config populated with sane defaults.
func Default() *Config {
	return &Config{
		Version:           1,
		MaxWorkers:        2,
		DefaultTimeoutSec: 300,
		SpawnIntervalSec:  5,
		LogRetentionDays:  7,
		SocketPath:        filepath.Join(GobrrDir(), "gobrrr.sock"),
		WorkspacePath:     filepath.Join(GobrrDir(), "workspace"),
		UptimeKuma: UptimeKumaConfig{
			IntervalSec: 60,
		},
	}
}

// Load reads the config file at path and merges it over defaults.
// If the file does not exist, defaults are returned without error.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// GobrrDir returns the gobrrr data directory. It respects the GOBRRR_DIR
// environment variable; if unset, it defaults to ~/.gobrrr.
func GobrrDir() string {
	if dir := os.Getenv("GOBRRR_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gobrrr"
	}
	return filepath.Join(home, ".gobrrr")
}
