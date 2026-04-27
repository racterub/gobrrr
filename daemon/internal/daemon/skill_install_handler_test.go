package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/racterub/gobrrr/internal/clawhub"
)

type fakeCommitter struct {
	approved   bool
	skipBinary bool
	calledReq  *clawhub.InstallRequest
	err        error
}

func (f *fakeCommitter) Commit(req clawhub.InstallRequest, decision clawhub.Decision) error {
	f.approved = decision.Approve
	f.skipBinary = decision.SkipBinary
	f.calledReq = &req
	return f.err
}

func TestSkillInstallHandler_Approve(t *testing.T) {
	fc := &fakeCommitter{}
	h := &skillInstallHandler{committer: fc}

	installReq := clawhub.InstallRequest{
		Slug:    "foo",
		Version: "1.0.0",
	}
	raw, _ := json.Marshal(installReq)

	req := &ApprovalRequest{
		ID:        "abcd",
		Kind:      "skill_install",
		Payload:   raw,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	require.NoError(t, h.Handle(req, "approve"))
	assert.True(t, fc.approved)
	assert.False(t, fc.skipBinary)
	assert.Equal(t, "foo", fc.calledReq.Slug)
}

func TestSkillInstallHandler_SkipBinary(t *testing.T) {
	fc := &fakeCommitter{}
	h := &skillInstallHandler{committer: fc}
	raw, _ := json.Marshal(clawhub.InstallRequest{})
	require.NoError(t, h.Handle(&ApprovalRequest{Payload: raw}, "skip_binary"))
	assert.True(t, fc.approved)
	assert.True(t, fc.skipBinary)
}

func TestSkillInstallHandler_Deny_CleansUp(t *testing.T) {
	fc := &fakeCommitter{}
	h := &skillInstallHandler{committer: fc}
	raw, _ := json.Marshal(clawhub.InstallRequest{})
	require.NoError(t, h.Handle(&ApprovalRequest{Payload: raw}, "deny"))
	assert.False(t, fc.approved)
}
