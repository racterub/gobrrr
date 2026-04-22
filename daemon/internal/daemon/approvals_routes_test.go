package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalsRoute_Decide_InvokesHandler(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	h := &fakeHandler{}
	d.Register("skill_install", h)

	req, err := d.Create("skill_install", "t", "b", []string{"approve", "deny"}, nil, time.Hour)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /approvals/{id}", approvalDecisionHandler(d))

	body, _ := json.Marshal(map[string]string{"decision": "approve"})
	r := httptest.NewRequest(http.MethodPost, "/approvals/"+req.ID, bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, []string{"approve"}, h.callsSnapshot())
}

func TestApprovalsRoute_MissingDecision_400(t *testing.T) {
	d := NewApprovalDispatcher(NewApprovalStore(t.TempDir()))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /approvals/{id}", approvalDecisionHandler(d))

	r := httptest.NewRequest(http.MethodPost, "/approvals/abcd", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApprovalsRoute_UnknownID_404(t *testing.T) {
	d := NewApprovalDispatcher(NewApprovalStore(t.TempDir()))
	d.Register("k", &fakeHandler{})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /approvals/{id}", approvalDecisionHandler(d))

	body, _ := json.Marshal(map[string]string{"decision": "approve"})
	r := httptest.NewRequest(http.MethodPost, "/approvals/missing", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestApprovalsStream_Rehydrates_ThenStreams(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	hub := NewApprovalHub()
	d := NewApprovalDispatcher(store)
	d.Register("k", &fakeHandler{})
	d.SetCallbacks(
		func(r *ApprovalRequest) { hub.Emit(ApprovalEvent{Type: ApprovalEventCreated, Request: r}) },
		func(id, dec string) {
			hub.Emit(ApprovalEvent{Type: ApprovalEventRemoved, ID: id, Decision: dec})
		},
	)

	// Preload: one approval that should be rehydrated on connect.
	_, err := d.Create("k", "pre", "body", []string{"approve"}, nil, time.Hour)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /approvals/stream", approvalStreamHandler(d, hub))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/approvals/stream")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	// Drop the initial ": connected" comment.
	_, _ = reader.ReadString('\n')
	_, _ = reader.ReadString('\n')

	// Rehydrated event.
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, line, `"type":"created"`)
	assert.Contains(t, line, `"title":"pre"`)
	_, _ = reader.ReadString('\n')

	// Trigger a live event.
	_, err = d.Create("k", "live", "body", []string{"approve"}, nil, time.Hour)
	require.NoError(t, err)

	line, err = reader.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, line, `"title":"live"`)
}
