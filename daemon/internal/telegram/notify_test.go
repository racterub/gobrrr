package telegram_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/racterub/gobrrr/internal/telegram"
)

func TestSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/sendMessage")
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "Hello from gobrrr")
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.Send("Hello from gobrrr")
	require.NoError(t, err)
}

func TestSendMessageAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"description":"Bad Request"}`)) //nolint:errcheck
	}))
	defer server.Close()

	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.Send("Hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Telegram API error")
}

func TestSendMarkdown(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.SendMarkdown("*bold text*")
	require.NoError(t, err)
	assert.Contains(t, capturedBody, "Markdown")
	assert.Contains(t, capturedBody, "*bold text*")
}

func TestSendLongMessage(t *testing.T) {
	var messageCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messageCount++
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	longMsg := strings.Repeat("a", 5000) // > 4096
	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.Send(longMsg)
	require.NoError(t, err)
	assert.Equal(t, 2, messageCount, "long message should be split into 2 chunks")
}

func TestSendExactlyMaxLength(t *testing.T) {
	var messageCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messageCount++
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	msg := strings.Repeat("b", 4096) // exactly at limit
	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.Send(msg)
	require.NoError(t, err)
	assert.Equal(t, 1, messageCount, "message at exactly max length should send as one chunk")
}

func TestSendEmojiHeavyMessage(t *testing.T) {
	// Telegram measures the 4096-character limit in UTF-16 code units, not
	// Unicode code points. Each astral-plane emoji rune encodes to 2 UTF-16
	// surrogate units, so a 4096-rune emoji chunk is 8192 UTF-16 units —
	// Telegram rejects it as "message is too long".
	var capturedTexts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Text string `json:"text"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		capturedTexts = append(capturedTexts, req.Text)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	msg := strings.Repeat("🎯", 5000) // 5000 emoji runes = 10000 UTF-16 units
	n := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	require.NoError(t, n.Send(msg))

	require.GreaterOrEqual(t, len(capturedTexts), 2, "5000-emoji input must split into multiple chunks")
	for i, chunk := range capturedTexts {
		units := len(utf16.Encode([]rune(chunk)))
		assert.LessOrEqualf(t, units, 4096, "chunk %d has %d UTF-16 units, exceeds Telegram limit", i, units)
	}
}

func TestSendBotTokenInURL(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	n := telegram.NewNotifier("123:MYTOKEN", "chat123", telegram.WithBaseURL(server.URL))
	err := n.Send("test")
	require.NoError(t, err)
	assert.Equal(t, "/bot123:MYTOKEN/sendMessage", capturedPath)
}
