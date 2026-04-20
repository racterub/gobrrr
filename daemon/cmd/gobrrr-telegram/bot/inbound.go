package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/permission"
)

const pairingTTL = 10 * time.Minute

func (w *Bot) handleUpdate(ctx context.Context, inner *tgbot.Bot, upd *models.Update) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: panic in handleUpdate: %v\n", r)
		}
	}()
	if upd.CallbackQuery != nil {
		w.handleCallbackQuery(ctx, upd.CallbackQuery)
		return
	}
	if upd.Message == nil {
		return
	}
	msg := upd.Message
	chatID := ChatIDToString(msg.Chat.ID)
	userID := ChatIDToString(msg.From.ID)
	isGroup := msg.Chat.Type == models.ChatTypeGroup || msg.Chat.Type == models.ChatTypeSupergroup

	a, err := w.store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: load access: %v\n", err)
		return
	}

	decision := access.Check(a, chatID, userID, isGroup, msg.Text, w.username)

	switch decision {
	case access.Drop:
		return
	case access.NeedPair:
		w.handlePairing(ctx, &a, chatID, userID, msg)
		return
	case access.Allow:
		// fall through to delivery
	}

	// Intercept permission-prompt replies ("y abcde" / "n abcde") before
	// they reach the MCP channel as a normal inbound message.
	if ok, yes, code := permission.Match(msg.Text); ok {
		if w.HandlePermissionReply(code, yes) {
			emoji := "👎"
			if yes {
				emoji = "👍"
			}
			_ = w.React(ctx, msg.Chat.ID, msg.ID, emoji)
			return
		}
	}

	attachPath, attachFileID, _ := w.maybeDownload(ctx, msg)
	if w.onInbound != nil {
		w.onInbound(ctx, upd, attachPath, attachFileID)
	}
	// Ack the inbound with a hardcoded reaction and remember it so the
	// first subsequent reply can swap it to the "done" reaction.
	if _, err := w.Inner().SetMessageReaction(ctx, (&reactParams{
		chatID:    msg.Chat.ID,
		messageID: msg.ID,
		emoji:     AckReactionEmoji,
	}).to()); err == nil {
		w.setPendingAck(msg.Chat.ID, msg.ID)
	}
}

func (w *Bot) handlePairing(ctx context.Context, a *access.Access, chatID, userID string, msg *models.Message) {
	// Pairing approval is intentionally out-of-band: the operator runs
	// `/telegram:access pair <code>` in their own terminal. Accepting
	// `y <code>` from Telegram would let the requester approve themselves.
	access.PruneExpired(a)
	code := access.NewPairingCode()
	now := time.Now()
	a.Pending[code] = access.Pending{
		SenderID:  userID,
		ChatID:    chatID,
		CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(pairingTTL).UnixMilli(),
	}
	if err := w.store.Save(*a); err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: save pairing: %v\n", err)
		return
	}
	w.sendText(ctx, msg.Chat.ID,
		msg.ID,
		fmt.Sprintf("pairing code: %s\nask the operator to run `/telegram:access pair %s` in their terminal to approve. expires in %s.",
			code, code, pairingTTL))
}

// reactParams is a tiny shim so the call site reads nicely; we build the
// real go-telegram/bot request in .to().
type reactParams struct {
	chatID    int64
	messageID int
	emoji     string
}

// to is defined in outbound.go (shares the go-telegram/bot types).
var _ = strconv.Itoa // keep import
