package scheduler_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAndListSchedule(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	sched, err := s.Create("morning", "0 8 * * *", "check email", "telegram", false)
	require.NoError(t, err)

	assert.NotEmpty(t, sched.ID)
	assert.Equal(t, "morning", sched.Name)
	assert.Equal(t, "0 8 * * *", sched.Cron)
	assert.Equal(t, "check email", sched.Prompt)
	assert.Equal(t, "telegram", sched.ReplyTo)
	assert.Nil(t, sched.LastFiredAt)

	list := s.List()
	assert.Len(t, list, 1)
}

func TestRemoveSchedule(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	sched, err := s.Create("test", "0 8 * * *", "do thing", "telegram", false)
	require.NoError(t, err)

	err = s.Remove(sched.Name)
	require.NoError(t, err)

	assert.Empty(t, s.List())
}

func TestRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	err := s.Remove("ghost")
	assert.Error(t, err)
}

func TestDuplicateNameRejected(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	_, err := s.Create("dup", "0 8 * * *", "a", "telegram", false)
	require.NoError(t, err)

	_, err = s.Create("dup", "0 9 * * *", "b", "telegram", false)
	assert.Error(t, err)
}

func TestPersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	s1 := scheduler.New(path, nil)
	_, err := s1.Create("persist", "0 8 * * *", "persist test", "telegram", false)
	require.NoError(t, err)

	s2 := scheduler.New(path, nil)
	err = s2.Load()
	require.NoError(t, err)

	list := s2.List()
	assert.Len(t, list, 1)
	assert.Equal(t, "persist", list[0].Name)
}

func TestCorruptedFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))

	s := scheduler.New(path, nil)
	err := s.Load()
	require.NoError(t, err)

	assert.Empty(t, s.List())
}

func TestCatchUpFires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	var mu sync.Mutex
	var submitted []string
	submitFn := func(prompt, replyTo string, allowWrites bool) error {
		mu.Lock()
		submitted = append(submitted, prompt)
		mu.Unlock()
		return nil
	}

	s := scheduler.New(path, submitFn)
	sched, err := s.Create("catchup", "0 * * * *", "hourly task", "telegram", false)
	require.NoError(t, err)

	ago := time.Now().Add(-90 * time.Minute)
	sched.LastFiredAt = &ago
	require.NoError(t, s.Save())

	s2 := scheduler.New(path, submitFn)
	require.NoError(t, s2.Load())
	s2.CatchUp()

	mu.Lock()
	assert.Len(t, submitted, 1)
	assert.Equal(t, "hourly task", submitted[0])
	mu.Unlock()
}

func TestCatchUpSkipsIfTooOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	var mu sync.Mutex
	var submitted []string
	submitFn := func(prompt, replyTo string, allowWrites bool) error {
		mu.Lock()
		submitted = append(submitted, prompt)
		mu.Unlock()
		return nil
	}

	s := scheduler.New(path, submitFn)
	sched, err := s.Create("old", "0 * * * *", "old task", "telegram", false)
	require.NoError(t, err)

	ago := time.Now().Add(-3 * time.Hour)
	sched.LastFiredAt = &ago
	require.NoError(t, s.Save())

	s2 := scheduler.New(path, submitFn)
	require.NoError(t, s2.Load())
	s2.CatchUp()

	mu.Lock()
	assert.Empty(t, submitted)
	mu.Unlock()
}
