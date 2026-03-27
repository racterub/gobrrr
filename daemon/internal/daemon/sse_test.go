package daemon

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEHub_SubscribeAndEmit(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	event := TaskResultEvent{
		TaskID:        "t_123",
		Status:        "completed",
		PromptSummary: "check gmail",
		Result:        "You have 3 unread emails",
		Error:         "",
		SubmittedAt:   time.Now().UTC(),
	}

	hub.Emit(event)

	select {
	case received := <-ch:
		assert.Equal(t, event.TaskID, received.TaskID)
		assert.Equal(t, event.Result, received.Result)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestSSEHub_SlowClientDropsEvents(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Fill the buffer (capacity 64)
	for i := 0; i < 70; i++ {
		hub.Emit(TaskResultEvent{TaskID: fmt.Sprintf("t_%d", i)})
	}

	// Should have received 64, dropped 6
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 64, count)
}

func TestSSEHub_UnsubscribeCleansUp(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	assert.Equal(t, 1, hub.ClientCount())

	hub.Unsubscribe(ch)
	assert.Equal(t, 0, hub.ClientCount())
}

func TestTaskResultEvent_JSON(t *testing.T) {
	event := TaskResultEvent{
		TaskID:        "t_123",
		Status:        "completed",
		PromptSummary: "check gmail for unread",
		Result:        "3 emails",
		Error:         "",
		SubmittedAt:   time.Date(2026, 3, 25, 1, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded TaskResultEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event.TaskID, decoded.TaskID)
	assert.Equal(t, event.PromptSummary, decoded.PromptSummary)
}

func TestTruncateRunes(t *testing.T) {
	assert.Equal(t, "hello", TruncateRunes("hello world", 5))
	assert.Equal(t, "你好", TruncateRunes("你好世界", 2))
	assert.Equal(t, "hi", TruncateRunes("hi", 100))
	assert.Equal(t, "", TruncateRunes("", 10))
}

func TestBuildTaskResultEvent(t *testing.T) {
	now := time.Now().UTC()
	errMsg := "something went wrong"
	task := &Task{
		ID:        "t_123",
		Prompt:    "check gmail for unread messages from alice",
		Status:    "failed",
		CreatedAt: now,
		Error:     &errMsg,
	}

	event := BuildTaskResultEvent(task, "partial output")
	assert.Equal(t, "t_123", event.TaskID)
	assert.Equal(t, "failed", event.Status)
	assert.Equal(t, "check gmail for unread messages from alice", event.PromptSummary)
	assert.Equal(t, "partial output", event.Result)
	assert.Equal(t, "something went wrong", event.Error)
	assert.Equal(t, now, event.SubmittedAt)

	task.Error = nil
	event = BuildTaskResultEvent(task, "result")
	assert.Equal(t, "", event.Error)
}
