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

// TelegramSessionConfig holds configuration for the managed Telegram session.
type TelegramSessionConfig struct {
	Enabled            bool     `json:"enabled"`
	MemoryCeilingMB    int      `json:"memory_ceiling_mb"`
	MaxUptimeHours     int      `json:"max_uptime_hours"`
	IdleThresholdMin   int      `json:"idle_threshold_min"`
	MaxRestartAttempts int      `json:"max_restart_attempts"`
	Channels           []string `json:"channels"`
}

// Config is the top-level daemon configuration.
type Config struct {
	Version           int              `json:"version"`
	MaxWorkers        int              `json:"max_workers"`
	DefaultTimeoutSec int              `json:"default_timeout_sec"`
	SpawnIntervalSec  int              `json:"spawn_interval_sec"`
	WarmWorkers       int              `json:"warm_workers"`
	LogRetentionDays  int              `json:"log_retention_days"`
	SocketPath        string           `json:"socket_path"`
	WorkspacePath     string           `json:"workspace_path"`
	Telegram          TelegramConfig        `json:"telegram"`
	UptimeKuma        UptimeKumaConfig      `json:"uptime_kuma"`
	TelegramSession   TelegramSessionConfig `json:"telegram_session"`
}

// defaultWorkspacePath returns ~/workspace as the default working directory
// for Claude sessions. This must be a directory already trusted by Claude Code
// to avoid the interactive "trust this folder" prompt in headless mode.
func defaultWorkspacePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "workspace"
	}
	return filepath.Join(home, "workspace")
}

// Default returns a Config populated with sane defaults.
func Default() *Config {
	return &Config{
		Version:           1,
		MaxWorkers:        2,
		DefaultTimeoutSec: 300,
		SpawnIntervalSec:  5,
		WarmWorkers:       1,
		LogRetentionDays:  7,
		SocketPath:        filepath.Join(GobrrDir(), "gobrrr.sock"),
		WorkspacePath:     defaultWorkspacePath(),
		UptimeKuma: UptimeKumaConfig{
			IntervalSec: 60,
		},
		TelegramSession: TelegramSessionConfig{
			Enabled:            false,
			MemoryCeilingMB:    3072,
			MaxUptimeHours:     6,
			IdleThresholdMin:   5,
			MaxRestartAttempts: 6,
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

	applyTelegramSessionDefaults(cfg)

	return cfg, nil
}

// applyTelegramSessionDefaults fills zero-value fields in TelegramSession with
// defaults. json.Unmarshal overwrites the entire nested struct when the key is
// present, zeroing fields absent from JSON, so defaults must be reapplied after
// unmarshal.
func applyTelegramSessionDefaults(cfg *Config) {
	d := Default().TelegramSession
	ts := &cfg.TelegramSession
	if ts.MemoryCeilingMB == 0 {
		ts.MemoryCeilingMB = d.MemoryCeilingMB
	}
	if ts.MaxUptimeHours == 0 {
		ts.MaxUptimeHours = d.MaxUptimeHours
	}
	if ts.IdleThresholdMin == 0 {
		ts.IdleThresholdMin = d.IdleThresholdMin
	}
	if ts.MaxRestartAttempts == 0 {
		ts.MaxRestartAttempts = d.MaxRestartAttempts
	}
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
