package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalHub_FanOut(t *testing.T) {
	hub := NewApprovalHub()
	c1 := hub.Subscribe()
	c2 := hub.Subscribe()
	defer hub.Unsubscribe(c1)
	defer hub.Unsubscribe(c2)

	hub.Emit(ApprovalEvent{Type: ApprovalEventCreated, Request: &ApprovalRequest{ID: "a"}})

	for _, ch := range []chan ApprovalEvent{c1, c2} {
		select {
		case ev := <-ch:
			assert.Equal(t, ApprovalEventCreated, ev.Type)
			assert.Equal(t, "a", ev.Request.ID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("no event received")
		}
	}
}

func TestDispatcher_EmitsVia_Hub(t *testing.T) {
	hub := NewApprovalHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	store := NewApprovalStore(t.TempDir())
	d := NewApprovalDispatcher(store)
	d.Register("k", &fakeHandler{})
	d.SetCallbacks(
		func(r *ApprovalRequest) { hub.Emit(ApprovalEvent{Type: ApprovalEventCreated, Request: r}) },
		func(id, dec, errMsg string) {
			hub.Emit(ApprovalEvent{Type: ApprovalEventRemoved, ID: id, Decision: dec, Error: errMsg})
		},
	)

	req, err := d.Create("k", "t", "b", []string{"approve"}, nil, time.Hour)
	require.NoError(t, err)

	ev := <-ch
	assert.Equal(t, ApprovalEventCreated, ev.Type)

	require.NoError(t, d.Decide(req.ID, "approve"))
	ev = <-ch
	assert.Equal(t, ApprovalEventRemoved, ev.Type)
	assert.Equal(t, req.ID, ev.ID)
	assert.Equal(t, "approve", ev.Decision)
}
