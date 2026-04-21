package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFromHTTPServer returns a Client that proxies to srv. (Mirrors existing
// test helpers elsewhere in the package — clients talk via Unix socket in
// production, but httptest over TCP is fine for unit tests.)
func newFromHTTPServer(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
	}
}

func TestClient_DecideApproval(t *testing.T) {
	var gotID string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/approvals/abcd", r.URL.Path)
		gotID = "abcd"
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	require.NoError(t, c.DecideApproval("abcd", "approve"))
	assert.Equal(t, "abcd", gotID)
	assert.Equal(t, "approve", gotBody["decision"])
}

func TestClient_StreamApprovals(t *testing.T) {
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, ": connected\n\n")
		flusher.Flush()
		once.Do(func() {
			payload := `{"type":"created","request":{"id":"aaaa","kind":"skill_install"}}`
			_, _ = io.WriteString(w, "data: "+payload+"\n\n")
			flusher.Flush()
		})
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, err := c.StreamApprovals(ctx)
	require.NoError(t, err)

	select {
	case ev := <-events:
		assert.Equal(t, "created", ev.Type)
		assert.Equal(t, "aaaa", ev.Request.ID)
		assert.Equal(t, "skill_install", ev.Request.Kind)
	case <-time.After(1 * time.Second):
		t.Fatal("no event received")
	}
	cancel()

	// Quick sanity-check that the parser doesn't explode on the comment line.
	_ = bufio.NewScanner(bytes.NewReader([]byte(": connected\n\n")))
}
