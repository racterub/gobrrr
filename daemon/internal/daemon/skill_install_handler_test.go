package daemon

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/racterub/gobrrr/internal/clawhub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCommitter struct {
	mu      sync.Mutex
	calls   int
	lastDec clawhub.Decision
	err     error
}

func (f *fakeCommitter) Commit(req clawhub.InstallRequest, d clawhub.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastDec = d
	return f.err
}

type fakeRefresher struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeRefresher) Refresh() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func newTestInstallRequest(t *testing.T) *ApprovalRequest {
	t.Helper()
	payload, err := json.Marshal(clawhub.InstallRequest{Slug: "test", Version: "1.0.0"})
	require.NoError(t, err)
	return &ApprovalRequest{ID: "test-id", Payload: payload}
}

func TestSkillInstallHandler_ApproveCommitsAndRefreshes(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.True(t, c.lastDec.Approve)
	assert.Equal(t, 1, r.calls, "Refresh must be called after a successful approve commit")
}

func TestSkillInstallHandler_SkipBinaryCommitsAndRefreshes(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "skip_binary")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.True(t, c.lastDec.Approve)
	assert.True(t, c.lastDec.SkipBinary)
	assert.Equal(t, 1, r.calls, "Refresh must be called after a successful skip_binary commit")
}

func TestSkillInstallHandler_DenySkipsRefresh(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "deny")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.False(t, c.lastDec.Approve)
	assert.Equal(t, 0, r.calls, "Refresh must not run on deny — nothing landed on disk")
}

func TestSkillInstallHandler_CommitErrorSkipsRefresh(t *testing.T) {
	commitErr := errors.New("commit failed")
	c := &fakeCommitter{err: commitErr}
	r := &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.Error(t, err)
	assert.ErrorIs(t, err, commitErr)
	assert.Equal(t, 1, c.calls)
	assert.Equal(t, 0, r.calls, "Refresh must not run when Commit fails — registry has nothing new to load")
}

func TestSkillInstallHandler_RefreshErrorIsSwallowed(t *testing.T) {
	c := &fakeCommitter{}
	r := &fakeRefresher{err: errors.New("refresh failed")}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.NoError(t, err, "Refresh failure must be best-effort: skill is already on disk")
	assert.Equal(t, 1, c.calls)
	assert.Equal(t, 1, r.calls)
}
