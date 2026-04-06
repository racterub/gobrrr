package bot

import (
	"context"
	"fmt"
	"os"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SendTextResult is one chunk's outcome.
type SendTextResult struct {
	MessageID int
	Err       error
}

// SendText sends a single text message. Caller handles chunking.
// replyTo=0 means no threading.
func (w *Bot) SendText(ctx context.Context, chatID int64, replyTo int, text string) (int, error) {
	params := &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}
	if replyTo != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: replyTo}
	}
	m, err := w.Inner().SendMessage(ctx, params)
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

// sendText is an internal convenience that logs rather than returning errors.
func (w *Bot) sendText(ctx context.Context, chatID int64, replyTo int, text string) {
	if _, err := w.SendText(ctx, chatID, replyTo, text); err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: sendText: %v\n", err)
	}
}

// EditText edits a previously-sent message.
func (w *Bot) EditText(ctx context.Context, chatID int64, messageID int, text string) error {
	_, err := w.Inner().EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	})
	return err
}

// React sets a single emoji reaction (empty string clears).
func (w *Bot) React(ctx context.Context, chatID int64, messageID int, emoji string) error {
	var reactions []models.ReactionType
	if emoji != "" {
		reactions = []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Type: "emoji", Emoji: emoji},
		}}
	}
	_, err := w.Inner().SetMessageReaction(ctx, &tgbot.SetMessageReactionParams{
		ChatID:    chatID,
		MessageID: messageID,
		Reaction:  reactions,
	})
	return err
}

func (r reactParams) to() *tgbot.SetMessageReactionParams {
	return &tgbot.SetMessageReactionParams{
		ChatID:    r.chatID,
		MessageID: r.messageID,
		Reaction: []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Type: "emoji", Emoji: r.emoji},
		}},
	}
}
