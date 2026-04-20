package daemon

import (
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
	assert.Equal(t, []string{"approve"}, h.calls)
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
