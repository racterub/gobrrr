package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// maybeDownload inspects the message for attachments and, if found, fetches
// the file into <stateDir>/inbox/. Returns (localPath, fileID, err). For
// photos localPath is set and fileID is also returned; for documents only
// the fileID+localPath. If nothing was attached, returns empty strings.
func (w *Bot) maybeDownload(ctx context.Context, msg *models.Message) (string, string, error) {
	var fileID, name string

	switch {
	case len(msg.Photo) > 0:
		// largest size is last
		fileID = msg.Photo[len(msg.Photo)-1].FileID
		name = fmt.Sprintf("photo-%d.jpg", msg.ID)
	case msg.Document != nil:
		fileID = msg.Document.FileID
		name = msg.Document.FileName
		if name == "" {
			name = fmt.Sprintf("doc-%d.bin", msg.ID)
		}
	default:
		return "", "", nil
	}

	inbox := filepath.Join(w.stateDir, "inbox")
	if err := os.MkdirAll(inbox, 0700); err != nil {
		return "", fileID, err
	}
	out := filepath.Join(inbox, fmt.Sprintf("%d-%d-%s", msg.Chat.ID, msg.ID, name))

	f, err := w.Inner().GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", fileID, err
	}
	url := w.Inner().FileDownloadLink(f)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return "", fileID, err
	}
	defer resp.Body.Close()
	fp, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fileID, err
	}
	defer fp.Close()
	if _, err := io.Copy(fp, resp.Body); err != nil {
		return "", fileID, err
	}
	return out, fileID, nil
}
