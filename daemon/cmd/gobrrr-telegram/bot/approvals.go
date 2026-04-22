package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/internal/client"
)

// RenderApprovalCard builds the Telegram message body + inline keyboard for an
// approval. Callback data is `ap:{id}:{action}` — kept short to fit Telegram's
// 64-byte callback_data cap with plenty of headroom.
func RenderApprovalCard(req *client.ApprovalRequest) (string, models.InlineKeyboardMarkup) {
	body := req.Title
	if req.Body != "" {
		body += "\n\n" + req.Body
	}

	// For skill_install, surface slug/version/sha from the payload without
	// importing the skill package (the bot is intentionally decoupled from
	// daemon-side types).
	if req.Kind == "skill_install" {
		var p struct {
			Slug    string `json:"slug"`
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		}
		if err := json.Unmarshal(req.Payload, &p); err == nil {
			if p.Slug != "" {
				body += fmt.Sprintf("\n\nSkill: %s@%s", p.Slug, p.Version)
			}
			if p.SHA256 != "" {
				body += fmt.Sprintf("\nsha256: %s", p.SHA256)
			}
		}
	}

	var row []models.InlineKeyboardButton
	for _, action := range req.Actions {
		row = append(row, models.InlineKeyboardButton{
			Text:         buttonLabel(action),
			CallbackData: "ap:" + req.ID + ":" + action,
		})
	}
	kb := models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
	return body, kb
}

func buttonLabel(action string) string {
	switch action {
	case "approve":
		return "✅ Approve"
	case "deny":
		return "❌ Deny"
	case "skip_binary":
		return "⏭️ Skip binary"
	}
	return action
}

// ParseApprovalCallback parses an inline-keyboard callback payload of the form
// `ap:{id}:{action}`. Returns (id, action, true) on match, (_,_,false) otherwise.
func ParseApprovalCallback(data string) (string, string, bool) {
	if !strings.HasPrefix(data, "ap:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(data, "ap:")
	i := strings.Index(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

type pendingApproval struct {
	chatID    int64
	messageID int
}

// approvalClient is the daemon-facing surface the subscriber needs. Satisfied
// by *client.Client in production; a narrow interface keeps the tests easy to
// fake without dragging in a full HTTP round-trip.
type approvalClient interface {
	StreamApprovals(ctx context.Context) (<-chan client.ApprovalEvent, error)
	DecideApproval(id, decision string) error
}

// ApprovalSubscriber is the bot-side runtime that (a) subscribes to the daemon's
// /approvals/stream, (b) renders + sends Telegram cards for `created` events,
// (c) posts the user's decision back to the daemon on callback, and (d) edits
// the message to show the resolution when `removed` events arrive.
type ApprovalSubscriber struct {
	bot    *Bot
	client approvalClient

	mu      sync.Mutex
	pending map[string]pendingApproval
}

func NewApprovalSubscriber(b *Bot, c approvalClient) *ApprovalSubscriber {
	return &ApprovalSubscriber{
		bot:     b,
		client:  c,
		pending: map[string]pendingApproval{},
	}
}

func (s *ApprovalSubscriber) trackPending(req *client.ApprovalRequest, chatID int64, messageID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[req.ID] = pendingApproval{chatID: chatID, messageID: messageID}
}

func (s *ApprovalSubscriber) hasPending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[id]
	return ok
}

func (s *ApprovalSubscriber) consumePending(id string) (int64, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	return p.chatID, p.messageID, ok
}

// Run connects to the daemon stream and processes events until ctx is
// cancelled. Intended to be launched in its own goroutine.
func (s *ApprovalSubscriber) Run(ctx context.Context) error {
	if s.bot == nil || s.client == nil {
		return fmt.Errorf("approval subscriber: nil bot or client")
	}
	events, err := s.client.StreamApprovals(ctx)
	if err != nil {
		return err
	}
	for ev := range events {
		switch ev.Type {
		case "created":
			s.handleCreated(ctx, ev.Request)
		case "removed":
			s.handleRemoved(ctx, ev.ID, ev.Decision)
		}
	}
	return nil
}

func (s *ApprovalSubscriber) handleCreated(ctx context.Context, req *client.ApprovalRequest) {
	if req == nil {
		return
	}
	a, err := s.bot.store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: approval %s: load access store: %v\n", req.ID, err)
		return
	}
	if a.OwnerChatID == "" {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: approval %s dropped: no owner_chat_id set — run the access skill to pair\n", req.ID)
		return
	}
	ownerID, err := strconv.ParseInt(a.OwnerChatID, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: approval %s: owner_chat_id %q is not a valid int64: %v\n", req.ID, a.OwnerChatID, err)
		return
	}
	body, kb := RenderApprovalCard(req)
	msg, err := s.bot.Inner().SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      ownerID,
		Text:        body,
		ReplyMarkup: kb,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: approval send: %v\n", err)
		return
	}
	s.trackPending(req, ownerID, msg.ID)
}

func (s *ApprovalSubscriber) handleRemoved(ctx context.Context, id, decision string) {
	chatID, messageID, ok := s.consumePending(id)
	if !ok {
		return
	}
	suffix := "\n\n❌ denied"
	switch decision {
	case "approve":
		suffix = "\n\n✅ approved"
	case "skip_binary":
		suffix = "\n\n⏭️ approved (binary skipped)"
	}
	// Edit the original body + clear the keyboard. Fetching the existing text is
	// unnecessary — we just append a resolution marker; Telegram accepts the call.
	_, _ = s.bot.Inner().EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	_, _ = s.bot.Inner().SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   "approval " + id + suffix,
	})
}

// HandleApprovalCallback posts the decision back to the daemon. Returns
// (handled, decision).
func (s *ApprovalSubscriber) HandleApprovalCallback(data string) (bool, string) {
	id, action, ok := ParseApprovalCallback(data)
	if !ok {
		return false, ""
	}
	if err := s.client.DecideApproval(id, action); err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: decide approval %s: %v\n", id, err)
		return true, action
	}
	return true, action
}
