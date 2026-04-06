package bot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbot "github.com/go-telegram/bot"
)

func TestSendTextHTTPTest(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"t","username":"testbot"}}`)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			// go-telegram/bot sends multipart/form-data
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			gotText = r.FormValue("text")
			io.WriteString(w, `{"ok":true,"result":{"message_id":42,"date":1,"chat":{"id":99,"type":"private"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := tgbot.New("TEST:TOKEN", tgbot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// getMe just to confirm wiring
	if _, err := b.GetMe(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: int64(99), Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 42 {
		t.Errorf("want ID 42, got %d", m.ID)
	}
	if gotText != "hello" {
		t.Errorf("want text=hello, got %q", gotText)
	}
}
