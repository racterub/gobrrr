package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneExpiredApprovals_SynthesizesDeny(t *testing.T) {
	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	h := &fakeHandler{}
	d.Register("skill_install", h)

	// An expired approval (ExpiresAt in the past).
	expired, err := d.Create("skill_install", "t", "b", []string{"approve", "deny"}, nil, -time.Minute)
	require.NoError(t, err)

	// A still-valid approval should be left alone.
	fresh, err := d.Create("skill_install", "t", "b", []string{"approve", "deny"}, nil, time.Hour)
	require.NoError(t, err)

	require.NoError(t, PruneExpiredApprovals(d))

	assert.Equal(t, []string{"deny"}, h.callsSnapshot())
	lastReq, _ := h.last()
	assert.Equal(t, expired.ID, lastReq.ID)

	// Fresh approval still there.
	got, err := store.Load(fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, fresh.ID, got.ID)
}
