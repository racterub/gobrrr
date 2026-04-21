package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApprovals_Integration_CreateStreamDecide exercises the full daemon-side
// contract: create via dispatcher, read SSE, POST decision, observe removal.
func TestApprovals_Integration_CreateStreamDecide(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	hub := NewApprovalHub()
	d := NewApprovalDispatcher(store)
	h := &fakeHandler{}
	d.Register("skill_install", h)
	d.SetCallbacks(
		func(r *ApprovalRequest) { hub.Emit(ApprovalEvent{Type: ApprovalEventCreated, Request: r}) },
		func(id, dec string) {
			hub.Emit(ApprovalEvent{Type: ApprovalEventRemoved, ID: id, Decision: dec})
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /approvals/{id}", approvalDecisionHandler(d))
	mux.HandleFunc("GET /approvals/stream", approvalStreamHandler(d, hub))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Start the SSE consumer.
	resp, err := http.Get(srv.URL + "/approvals/stream")
	require.NoError(t, err)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	_, _ = reader.ReadString('\n')
	_, _ = reader.ReadString('\n')

	// Create.
	req, err := d.Create("skill_install", "install foo", "body",
		[]string{"approve", "deny"}, map[string]string{"slug": "foo"}, time.Hour)
	require.NoError(t, err)

	created, err := reader.ReadString('\n')
	require.NoError(t, err)
	_, _ = reader.ReadString('\n')
	assert.Contains(t, created, `"type":"created"`)
	assert.Contains(t, created, req.ID)

	// Decide via HTTP.
	body, _ := json.Marshal(map[string]string{"decision": "approve"})
	decResp, err := http.Post(srv.URL+"/approvals/"+req.ID, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	decResp.Body.Close()
	assert.Equal(t, http.StatusNoContent, decResp.StatusCode)

	removed, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, removed, `"type":"removed"`)
	assert.Contains(t, removed, req.ID)
	assert.Contains(t, removed, `"decision":"approve"`)

	// Handler was called with the approve decision.
	assert.Equal(t, []string{"approve"}, h.calls)
	assert.True(t, strings.Contains(string(h.lastReq.Payload), "foo"))
}
