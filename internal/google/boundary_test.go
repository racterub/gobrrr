package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapEmail(t *testing.T) {
	wrapped := WrapEmail("alice@example.com", "Hello", "Hi there")
	assert.Contains(t, wrapped, "UNTRUSTED")
	assert.Contains(t, wrapped, "alice@example.com")
	assert.Contains(t, wrapped, "Hello")
	assert.Contains(t, wrapped, "Hi there")
	assert.Contains(t, wrapped, "EMAIL DATA START")
	assert.Contains(t, wrapped, "EMAIL DATA END")
}

func TestWrapEmail_PreservesMultilineBody(t *testing.T) {
	body := "Line 1\nLine 2\nLine 3"
	wrapped := WrapEmail("bob@example.com", "Multi", body)
	assert.Contains(t, wrapped, body)
}

func TestWrapCalendarEvent(t *testing.T) {
	wrapped := WrapCalendarEvent("Team Meeting", "Discuss roadmap", "2026-03-24T10:00:00Z", "2026-03-24T11:00:00Z")
	assert.Contains(t, wrapped, "UNTRUSTED")
	assert.Contains(t, wrapped, "Team Meeting")
	assert.Contains(t, wrapped, "Discuss roadmap")
	assert.Contains(t, wrapped, "2026-03-24T10:00:00Z")
	assert.Contains(t, wrapped, "2026-03-24T11:00:00Z")
	assert.Contains(t, wrapped, "CALENDAR DATA START")
	assert.Contains(t, wrapped, "CALENDAR DATA END")
}
