// Package bot wraps go-telegram/bot to integrate with the Telegram channel's
// access gate and MCP notification emitter.
package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
)

// Hardcoded reaction emojis. Telegram only accepts a fixed set of emoji for
// bot reactions; both of these are in the standard allowed list.
const (
	AckReactionEmoji  = "👀"
	DoneReactionEmoji = "👍"
)

// InboundHandler receives a gated, already-approved message. Implementations
// emit it as an MCP channel notification.
type InboundHandler func(ctx context.Context, u *models.Update, attachPath, attachFileID string)

// Bot is the Telegram client used by both the long-poll loop (inbound) and
// the MCP tool handlers (outbound).
type Bot struct {
	b         *tgbot.Bot
	username  string
	store     *access.Store
	stateDir  string
	onInbound InboundHandler

	// pendingAck tracks the most recent ack'd inbound message per chat so
	// that the first reply in that chat can swap the reaction to "done".
	pendingMu  sync.Mutex
	pendingAck map[int64]int // chatID → messageID
}

// ConsumePendingAck returns and clears the pending ack message_id for a chat,
// if any. Used by the reply tool to swap the ack reaction to a done reaction
// after a successful send.
func (w *Bot) ConsumePendingAck(chatID int64) (int, bool) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	mid, ok := w.pendingAck[chatID]
	if ok {
		delete(w.pendingAck, chatID)
	}
	return mid, ok
}

// ClearPendingAck drops any pending ack for a chat without swapping. Used by
// the react tool so manual reactions win over the auto-swap.
func (w *Bot) ClearPendingAck(chatID int64) {
	w.pendingMu.Lock()
	delete(w.pendingAck, chatID)
	w.pendingMu.Unlock()
}

func (w *Bot) setPendingAck(chatID int64, messageID int) {
	w.pendingMu.Lock()
	if w.pendingAck == nil {
		w.pendingAck = map[int64]int{}
	}
	w.pendingAck[chatID] = messageID
	w.pendingMu.Unlock()
}

func New(token, stateDir string, store *access.Store, onInbound InboundHandler) (*Bot, error) {
	wrapped := &Bot{store: store, stateDir: stateDir, onInbound: onInbound}
	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(wrapped.handleUpdate),
	}
	inner, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, err
	}
	wrapped.b = inner
	return wrapped, nil
}

func (w *Bot) Username() string { return w.username }

// Start fetches getMe and runs the long-poll loop until ctx is cancelled.
func (w *Bot) Start(ctx context.Context) error {
	me, err := w.b.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	w.username = me.Username
	fmt.Fprintf(os.Stderr, "gobrrr-telegram: connected as @%s\n", w.username)
	w.b.Start(ctx) // blocks
	return nil
}

// Inner returns the raw go-telegram/bot instance for outbound tool handlers.
func (w *Bot) Inner() *tgbot.Bot { return w.b }

// ChatIDToString converts any Telegram chat ID (which may arrive as int64)
// to the canonical string form used by access.json.
func ChatIDToString(id int64) string { return strconv.FormatInt(id, 10) }
