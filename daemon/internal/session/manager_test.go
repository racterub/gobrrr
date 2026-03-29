package session_test

import (
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() config.TelegramSessionConfig {
	return config.TelegramSessionConfig{
		Enabled:            true,
		MemoryCeilingMB:    3072,
		MaxUptimeHours:     6,
		IdleThresholdMin:   5,
		MaxRestartAttempts: 6,
		Channels:           []string{"plugin:telegram@claude-plugins-official"},
	}
}

func TestNewManager(t *testing.T) {
	m := session.NewManager(testConfig(), nil)
	require.NotNil(t, m)
	assert.False(t, m.Running())
}

func TestBackoffProgression(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	assert.Equal(t, 30*time.Second, m.BackoffFor(0))
	assert.Equal(t, 60*time.Second, m.BackoffFor(1))
	assert.Equal(t, 120*time.Second, m.BackoffFor(2))
	assert.Equal(t, 300*time.Second, m.BackoffFor(3))
	assert.Equal(t, 300*time.Second, m.BackoffFor(4)) // capped
}

func TestShouldRotateMemory(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	reason, rotate := m.EvalRotation(4000, 1*time.Hour, 0*time.Minute)
	assert.True(t, rotate)
	assert.Contains(t, reason, "memory")
}

func TestShouldRotateUptimeWhenIdle(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	reason, rotate := m.EvalRotation(1000, 7*time.Hour, 10*time.Minute)
	assert.True(t, rotate)
	assert.Contains(t, reason, "uptime")
}

func TestShouldNotRotateUptimeWhenActive(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	_, rotate := m.EvalRotation(1000, 7*time.Hour, 1*time.Minute)
	assert.False(t, rotate)
}

func TestShouldNotRotateNormal(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	_, rotate := m.EvalRotation(1000, 1*time.Hour, 0*time.Minute)
	assert.False(t, rotate)
}
