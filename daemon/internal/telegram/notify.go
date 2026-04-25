// Package telegram provides a Telegram Bot API notifier.
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf16"
)

const (
	defaultBaseURL = "https://api.telegram.org"
	maxMessageLen  = 4096
)

// Notifier sends messages via the Telegram Bot API.
type Notifier struct {
	token   string
	chatID  string
	baseURL string
}

// Option is a functional option for Notifier.
type Option func(*Notifier)

// WithBaseURL overrides the Telegram API base URL. Used for testing.
func WithBaseURL(url string) Option {
	return func(n *Notifier) {
		n.baseURL = url
	}
}

// NewNotifier creates a new Notifier for the given bot token and chat ID.
func NewNotifier(token, chatID string, opts ...Option) *Notifier {
	n := &Notifier{
		token:   token,
		chatID:  chatID,
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// sendMessageRequest is the JSON body for the Telegram sendMessage API.
type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Send sends a plain text message. Messages longer than 4096 characters are
// split at the boundary and sent as multiple messages.
func (n *Notifier) Send(text string) error {
	return n.sendWithParseMode(text, "")
}

// SendMarkdown sends a message with Markdown parse mode. Messages longer than
// 4096 characters are split at the boundary and sent as multiple messages.
func (n *Notifier) SendMarkdown(text string) error {
	return n.sendWithParseMode(text, "Markdown")
}

// sendWithParseMode splits text into chunks and sends each chunk.
func (n *Notifier) sendWithParseMode(text, parseMode string) error {
	chunks := splitMessage(text, maxMessageLen)
	for _, chunk := range chunks {
		if err := n.sendChunk(chunk, parseMode); err != nil {
			return err
		}
	}
	return nil
}

// sendChunk sends a single message chunk to the Telegram API.
func (n *Notifier) sendChunk(text, parseMode string) error {
	reqBody := sendMessageRequest{
		ChatID:    n.chatID,
		Text:      text,
		ParseMode: parseMode,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.token)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("posting to Telegram: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Telegram returns {"ok":true,...} on success.
	var apiResp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("parsing Telegram response: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("Telegram API error: %s", string(body))
	}
	return nil
}

// splitMessage splits text into chunks no longer than maxLen UTF-16 code
// units, since that's what Telegram's 4096-character limit measures.
// Splits happen on rune boundaries.
func splitMessage(text string, maxLen int) []string {
	var chunks []string
	start := 0
	unitCount := 0
	for i, r := range text {
		size := utf16.RuneLen(r)
		if size < 0 {
			size = 1 // utf8.RuneError; conservative single unit
		}
		if unitCount+size > maxLen {
			chunks = append(chunks, text[start:i])
			start = i
			unitCount = 0
		}
		unitCount += size
	}
	chunks = append(chunks, text[start:])
	return chunks
}
