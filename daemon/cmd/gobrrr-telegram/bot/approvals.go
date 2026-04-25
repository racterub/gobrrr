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
	var b strings.Builder
	b.WriteString(req.Title)
	if req.Body != "" {
		b.WriteString("\n\n")
		b.WriteString(req.Body)
	}

	if req.Kind == "skill_install" {
		renderSkillInstallBody(&b, req.Payload)
	}

	var row []models.InlineKeyboardButton
	for _, action := range req.Actions {
		row = append(row, models.InlineKeyboardButton{
			Text:         buttonLabel(action),
			CallbackData: "ap:" + req.ID + ":" + action,
		})
	}
	kb := models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
	return b.String(), kb
}

// skillInstallPayload mirrors the fields of clawhub.InstallRequest that the
// card surfaces. Defined locally so the bot stays decoupled from daemon-side
// types — extra fields on the wire are ignored silently.
type skillInstallPayload struct {
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	SourceURL   string `json:"source_url"`
	SHA256      string `json:"sha256"`
	Frontmatter struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Metadata    struct {
			OpenClaw struct {
				Requires struct {
					ToolPermissions struct {
						Read  []string `json:"read"`
						Write []string `json:"write"`
					} `json:"tool_permissions"`
				} `json:"requires"`
			} `json:"openclaw"`
		} `json:"metadata"`
	} `json:"frontmatter"`
	ProposedCommands []struct {
		Command string `json:"command"`
	} `json:"proposed_commands"`
}

func renderSkillInstallBody(b *strings.Builder, payload json.RawMessage) {
	var p skillInstallPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	name := p.Frontmatter.Name
	if name == "" {
		name = p.Slug
	}
	if name != "" {
		b.WriteString("\n\n")
		b.WriteString(name)
		if p.Version != "" {
			b.WriteString(" v")
			b.WriteString(p.Version)
		}
	}
	if desc := p.Frontmatter.Description; desc != "" {
		b.WriteString("\n")
		b.WriteString(desc)
	}

	if p.SourceURL != "" {
		b.WriteString("\n\nSource: ")
		b.WriteString(p.SourceURL)
	}
	if p.SHA256 != "" {
		sha := p.SHA256
		if len(sha) > 8 {
			sha = sha[:8]
		}
		b.WriteString("\nsha256: ")
		b.WriteString(sha)
	}

	if len(p.ProposedCommands) > 0 {
		b.WriteString("\n\nBinaries to install:")
		for _, c := range p.ProposedCommands {
			if c.Command == "" {
				continue
			}
			b.WriteString("\n  • ")
			b.WriteString(c.Command)
		}
	}

	tp := p.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions
	if len(tp.Read) > 0 || len(tp.Write) > 0 {
		b.WriteString("\n\nPermissions:")
		if len(tp.Read) > 0 {
			b.WriteString("\n  read:  ")
			b.WriteString(strings.Join(tp.Read, ", "))
		}
		if len(tp.Write) > 0 {
			b.WriteString("\n  write: ")
			b.WriteString(strings.Join(tp.Write, ", "))
		}
	}
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
	title     string
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
	s.pending[req.ID] = pendingApproval{chatID: chatID, messageID: messageID, title: req.Title}
}

func (s *ApprovalSubscriber) hasPending(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[id]
	return ok
}

func (s *ApprovalSubscriber) consumePending(id string) (pendingApproval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	return p, ok
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
			s.handleRemoved(ctx, ev.ID, ev.Decision, ev.Error)
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

func (s *ApprovalSubscriber) handleRemoved(ctx context.Context, id, decision, errMsg string) {
	p, ok := s.consumePending(id)
	if !ok {
		return
	}
	// A non-empty errMsg means the per-kind handler failed (e.g. skill commit
	// errored after the user approved). Surface that explicitly so the user
	// doesn't see ✅ for an action that didn't actually take effect.
	suffix := "\n\n❌ denied"
	switch {
	case errMsg != "":
		suffix = "\n\n⚠️ failed: " + errMsg
	case decision == "approve":
		suffix = "\n\n✅ approved"
	case decision == "skip_binary":
		suffix = "\n\n⏭️ approved (binary skipped)"
	}
	// Prefer the original request title ("install skill foo@1.0.0") so the user
	// sees what was actually decided; fall back to the ID only when the bot was
	// restarted and lost the in-memory title.
	label := p.title
	if label == "" {
		label = "approval " + id
	}
	// Edit the original body + clear the keyboard. Fetching the existing text is
	// unnecessary — we just append a resolution marker; Telegram accepts the call.
	_, _ = s.bot.Inner().EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
		ChatID:      p.chatID,
		MessageID:   p.messageID,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	_, _ = s.bot.Inner().SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: p.chatID,
		Text:   label + suffix,
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
