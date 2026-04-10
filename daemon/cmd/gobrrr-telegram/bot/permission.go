package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
)

const (
	permApprovalTimeout = 2 * time.Minute
	maxPermInputDisplay = 1500
)

type permEntry struct {
	requestID string
	chatID    int64
	messageID int
	body      string
	timer     *time.Timer
}

// HandlePermissionRequest sends a Telegram approval prompt to the configured
// owner chat. On missing config or send failure, auto-denies via the
// onPermissionReply callback.
func (w *Bot) HandlePermissionRequest(ctx context.Context, requestID, toolName, description, inputPreview string) {
	a, err := w.store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: permission load access: %v\n", err)
		w.fireReply(requestID, false)
		return
	}
	if a.OwnerChatID == "" {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: permission: no ownerChatId set\n")
		w.fireReply(requestID, false)
		return
	}
	ownerID, err := strconv.ParseInt(a.OwnerChatID, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: permission: bad ownerChatId %q: %v\n", a.OwnerChatID, err)
		w.fireReply(requestID, false)
		return
	}

	code := access.NewPairingCode()
	body := formatPermissionBody(toolName, description, inputPreview, code)
	msg, err := w.Inner().SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      ownerID,
		Text:        body,
		ParseMode:   models.ParseModeMarkdownV1,
		ReplyMarkup: permissionKeyboard(code),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: permission send: %v\n", err)
		w.fireReply(requestID, false)
		return
	}

	entry := &permEntry{
		requestID: requestID,
		chatID:    ownerID,
		messageID: msg.ID,
		body:      body,
	}
	w.permMu.Lock()
	if w.permPending == nil {
		w.permPending = map[string]*permEntry{}
	}
	w.permPending[code] = entry
	w.permMu.Unlock()

	entry.timer = time.AfterFunc(permApprovalTimeout, func() {
		w.resolvePermission(code, false, "\n\n⏱️ auto-denied after 2m")
	})
}

// HandlePermissionReply is called from the inbound handler when the operator
// sends a `y <code>` / `n <code>` reply. Returns true if a pending entry was
// found and resolved.
func (w *Bot) HandlePermissionReply(code string, allow bool) bool {
	suffix := "\n\n❌ denied"
	if allow {
		suffix = "\n\n✅ approved"
	}
	return w.resolvePermission(code, allow, suffix)
}

func (w *Bot) resolvePermission(code string, allow bool, suffix string) bool {
	w.permMu.Lock()
	entry, ok := w.permPending[code]
	if ok {
		delete(w.permPending, code)
	}
	w.permMu.Unlock()
	if !ok {
		return false
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	_, _ = w.Inner().EditMessageText(context.Background(), &tgbot.EditMessageTextParams{
		ChatID:      entry.chatID,
		MessageID:   entry.messageID,
		Text:        entry.body + suffix,
		ParseMode:   models.ParseModeMarkdownV1,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	w.fireReply(entry.requestID, allow)
	return true
}

// HandlePermissionCallback parses an inline-button callback data string
// ("pa:CODE" / "pd:CODE") and resolves the matching pending entry. Returns
// (handled, allow) so the caller can answer the callback query.
func (w *Bot) HandlePermissionCallback(data string) (bool, bool) {
	var allow bool
	switch {
	case strings.HasPrefix(data, "pa:"):
		allow = true
	case strings.HasPrefix(data, "pd:"):
		allow = false
	default:
		return false, false
	}
	code := strings.TrimPrefix(strings.TrimPrefix(data, "pa:"), "pd:")
	return w.HandlePermissionReply(code, allow), allow
}

func (w *Bot) handleCallbackQuery(ctx context.Context, cq *models.CallbackQuery) {
	handled, allow := w.HandlePermissionCallback(cq.Data)
	text := ""
	if handled {
		if allow {
			text = "approved"
		} else {
			text = "denied"
		}
	} else {
		text = "expired"
	}
	_, _ = w.Inner().AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
		CallbackQueryID: cq.ID,
		Text:            text,
	})
}

func permissionKeyboard(code string) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "✅ Approve", CallbackData: "pa:" + code},
			{Text: "❌ Deny", CallbackData: "pd:" + code},
		}},
	}
}

func (w *Bot) fireReply(requestID string, allow bool) {
	if w.onPermissionReply != nil {
		w.onPermissionReply(requestID, allow)
	}
}

func formatPermissionBody(toolName, description, inputPreview, code string) string {
	display := inputPreview
	if len(display) > maxPermInputDisplay {
		display = display[:maxPermInputDisplay] + "\n…(truncated)"
	}
	desc := ""
	if description != "" {
		desc = "\n" + description
	}
	return fmt.Sprintf(
		"🔐 approve tool call?\ntool: %s%s\ninput:\n```\n%s\n```\nreply `y %s` to allow, `n %s` to deny (auto-deny in 2m)",
		toolName, desc, display, code, code,
	)
}
