package security_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/racterub/gobrrr/internal/security"
)

func TestConfirmationApproved(t *testing.T) {
	gate := security.NewGate(5 * time.Second)
	gate.Request("t_123")
	go func() {
		time.Sleep(50 * time.Millisecond)
		gate.Approve("t_123") //nolint:errcheck
	}()
	decision := gate.Wait("t_123")
	assert.True(t, decision.Approved)
}

func TestConfirmationDenied(t *testing.T) {
	gate := security.NewGate(5 * time.Second)
	gate.Request("t_123")
	go func() {
		time.Sleep(50 * time.Millisecond)
		gate.Deny("t_123") //nolint:errcheck
	}()
	decision := gate.Wait("t_123")
	assert.False(t, decision.Approved)
	assert.Equal(t, "denied", decision.Reason)
}

func TestConfirmationTimeout(t *testing.T) {
	gate := security.NewGate(100 * time.Millisecond)
	gate.Request("t_123")
	decision := gate.Wait("t_123")
	assert.False(t, decision.Approved)
	assert.Equal(t, "timeout", decision.Reason)
}

func TestApproveUnknownTask(t *testing.T) {
	gate := security.NewGate(time.Second)
	err := gate.Approve("nonexistent")
	assert.Error(t, err)
}

func TestDenyUnknownTask(t *testing.T) {
	gate := security.NewGate(time.Second)
	err := gate.Deny("nonexistent")
	assert.Error(t, err)
}

func TestCleanup(t *testing.T) {
	gate := security.NewGate(time.Second)
	gate.Request("t_123")
	gate.Cleanup("t_123")
	err := gate.Approve("t_123")
	assert.Error(t, err) // already cleaned up
}
