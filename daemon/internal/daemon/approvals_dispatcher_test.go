package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHandler struct {
	mu      sync.Mutex
	calls   []string
	err     error
	lastReq *ApprovalRequest
	lastDec string
}

func (f *fakeHandler) Handle(req *ApprovalRequest, decision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, decision)
	f.lastReq = req
	f.lastDec = decision
	return f.err
}

// callsSnapshot returns a copy of calls under lock so tests can assert without
// racing with a Handle invocation that may still be writing (the SSE/HTTP
// paths resolve handler calls off the goroutine that reads the result).
func (f *fakeHandler) callsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// last returns the most recent (req, decision) pair under lock.
func (f *fakeHandler) last() (*ApprovalRequest, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq, f.lastDec
}

func TestDispatcher_CreateDecide_FiresHandler(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	h := &fakeHandler{}
	d.Register("skill_install", h)

	req, err := d.Create("skill_install", "title", "body",
		[]string{"approve", "deny"}, map[string]string{"slug": "foo"}, time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, req.ID)

	require.NoError(t, d.Decide(req.ID, "approve"))
	assert.Equal(t, []string{"approve"}, h.callsSnapshot())
	lastReq, _ := h.last()
	assert.Equal(t, "foo", mustString(lastReq.Payload, "slug"))

	// file is gone after Decide
	_, err = store.Load(req.ID)
	assert.True(t, errors.Is(err, errFileNotExist()) || isErrNotExist(err))
}

func TestDispatcher_UnknownID_Returns_ErrApprovalNotFound(t *testing.T) {
	d := NewApprovalDispatcher(NewApprovalStore(t.TempDir()))
	err := d.Decide("nope", "approve")
	assert.ErrorIs(t, err, ErrApprovalNotFound)
}

func TestDispatcher_UnknownKind_Returns_ErrUnknownApprovalKind(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	// no handler registered
	req, err := d.Create("mystery", "t", "b", []string{"approve"}, nil, time.Hour)
	require.NoError(t, err)
	err = d.Decide(req.ID, "approve")
	assert.ErrorIs(t, err, ErrUnknownApprovalKind)
}

func TestDispatcher_CreateEmits_OnCreate_Callback(t *testing.T) {
	d := NewApprovalDispatcher(NewApprovalStore(t.TempDir()))
	var got *ApprovalRequest
	d.SetCallbacks(func(r *ApprovalRequest) { got = r }, nil)
	_, err := d.Create("k", "t", "b", []string{"approve"}, nil, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "k", got.Kind)
}

func TestDispatcher_DecideEmits_OnRemove_Callback(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	d.Register("k", &fakeHandler{})
	var gotID, gotDec, gotErr string
	d.SetCallbacks(nil, func(id, dec, errMsg string) { gotID, gotDec, gotErr = id, dec, errMsg })
	req, err := d.Create("k", "t", "b", []string{"approve"}, nil, time.Minute)
	require.NoError(t, err)
	require.NoError(t, d.Decide(req.ID, "approve"))
	assert.Equal(t, req.ID, gotID)
	assert.Equal(t, "approve", gotDec)
	assert.Equal(t, "", gotErr, "successful Decide should report empty error message")
}

// TestDispatcher_HandlerError_PropagatesToCallback locks in the contract that a
// handler error is surfaced to onRemove subscribers (and ultimately SSE
// listeners) instead of being silently swallowed. Without this, a Telegram bot
// rendering "removed" events would show ✅ for an install that actually failed
// to commit.
func TestDispatcher_HandlerError_PropagatesToCallback(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	d.Register("k", &fakeHandler{err: errors.New("commit failed")})

	var gotID, gotDec, gotErr string
	d.SetCallbacks(nil, func(id, dec, errMsg string) {
		gotID, gotDec, gotErr = id, dec, errMsg
	})

	req, err := d.Create("k", "t", "b", []string{"approve"}, nil, time.Minute)
	require.NoError(t, err)

	decideErr := d.Decide(req.ID, "approve")
	require.Error(t, decideErr)
	assert.Contains(t, decideErr.Error(), "commit failed")

	assert.Equal(t, req.ID, gotID)
	assert.Equal(t, "approve", gotDec)
	assert.Equal(t, "commit failed", gotErr)
}

func mustString(raw json.RawMessage, key string) string {
	var m map[string]string
	_ = json.Unmarshal(raw, &m)
	return m[key]
}

func isErrNotExist(err error) bool { return os.IsNotExist(err) }
func errFileNotExist() error       { return os.ErrNotExist }
