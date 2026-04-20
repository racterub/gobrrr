package bot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
)

// mockTelegramServer spins up an httptest server that answers the handful of
// Bot API endpoints handlePairing can reach (getMe + sendMessage). It's a
// black-hole for everything else so the test fails loudly on unexpected
// calls.
func mockTelegramServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"t","username":"testbot"}}`)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			io.WriteString(w, `{"ok":true,"result":{"message_id":1,"date":1,"chat":{"id":42,"type":"private"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestBot(t *testing.T, serverURL string, store *access.Store) *Bot {
	t.Helper()
	inner, err := tgbot.New("TEST:TOKEN", tgbot.WithServerURL(serverURL))
	if err != nil {
		t.Fatal(err)
	}
	return &Bot{b: inner, store: store}
}

// TestHandlePairing_RequesterCannotSelfApprove is a regression test for the
// security flaw where the requester of a pairing could reply `y <code>` in
// their own chat and be added to AllowFrom without operator involvement.
// Approval must happen out-of-band via the `/telegram:access` skill.
func TestHandlePairing_RequesterCannotSelfApprove(t *testing.T) {
	dir := t.TempDir()
	store := access.NewStore(dir, false)
	a := access.Default()
	a.Pending["abcde"] = access.Pending{
		SenderID:  "42",
		ChatID:    "42",
		CreatedAt: time.Now().UnixMilli(),
		ExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli(),
	}
	if err := store.Save(a); err != nil {
		t.Fatal(err)
	}

	srv := mockTelegramServer(t)
	defer srv.Close()
	b := newTestBot(t, srv.URL, store)

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	b.handlePairing(context.Background(), &loaded, "42", "42", &models.Message{
		ID:   1,
		Chat: models.Chat{ID: 42},
		Text: "y abcde",
	})

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got.AllowFrom, "42") {
		t.Fatal("security regression: requester was added to AllowFrom via self-reply")
	}
}

// TestHandlePairing_IssuesCodeForNewDM exercises the happy path: an unpaired
// user's first DM produces a pending code and a message pointing the operator
// at the /telegram:access terminal flow.
func TestHandlePairing_IssuesCodeForNewDM(t *testing.T) {
	dir := t.TempDir()
	store := access.NewStore(dir, false)
	if err := store.Save(access.Default()); err != nil {
		t.Fatal(err)
	}

	srv := mockTelegramServer(t)
	defer srv.Close()
	b := newTestBot(t, srv.URL, store)

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	b.handlePairing(context.Background(), &loaded, "42", "42", &models.Message{
		ID:   1,
		Chat: models.Chat{ID: 42},
		Text: "hello",
	})

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 1 {
		t.Fatalf("expected 1 pending entry, got %d", len(got.Pending))
	}
	if slices.Contains(got.AllowFrom, "42") {
		t.Fatal("plain DM must not grant access")
	}
}
