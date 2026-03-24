package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockCalendarAPI implements CalendarAPI for use in tests.
type MockCalendarAPI struct {
	Events    []*EventSummary
	Detail    *EventDetail
	Created   []*EventSummary
	Deleted   map[string]bool
	Updated   map[string]*EventSummary
	TodayErr  error
	WeekErr   error
	GetErr    error
	CreateErr error
	UpdateErr error
	DeleteErr error
}

func (m *MockCalendarAPI) Today() ([]*EventSummary, error) {
	if m.TodayErr != nil {
		return nil, m.TodayErr
	}
	return m.Events, nil
}

func (m *MockCalendarAPI) Week() ([]*EventSummary, error) {
	if m.WeekErr != nil {
		return nil, m.WeekErr
	}
	return m.Events, nil
}

func (m *MockCalendarAPI) GetEvent(eventID string) (*EventDetail, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	return m.Detail, nil
}

func (m *MockCalendarAPI) CreateEvent(title, start, end, description string) error {
	if m.CreateErr != nil {
		return m.CreateErr
	}
	if m.Created == nil {
		m.Created = []*EventSummary{}
	}
	m.Created = append(m.Created, &EventSummary{
		Title:       title,
		Start:       start,
		End:         end,
		Description: description,
	})
	return nil
}

func (m *MockCalendarAPI) UpdateEvent(eventID, title, start, end string) error {
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	if m.Updated == nil {
		m.Updated = map[string]*EventSummary{}
	}
	m.Updated[eventID] = &EventSummary{
		ID:    eventID,
		Title: title,
		Start: start,
		End:   end,
	}
	return nil
}

func (m *MockCalendarAPI) DeleteEvent(eventID string) error {
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	if m.Deleted == nil {
		m.Deleted = map[string]bool{}
	}
	m.Deleted[eventID] = true
	return nil
}

func TestListEventsToday(t *testing.T) {
	mock := &MockCalendarAPI{
		Events: []*EventSummary{
			{
				ID:    "evt1",
				Title: "Standup",
				Start: "2026-03-24T09:00:00Z",
				End:   "2026-03-24T09:30:00Z",
			},
			{
				ID:    "evt2",
				Title: "Lunch",
				Start: "2026-03-24T12:00:00Z",
				End:   "2026-03-24T13:00:00Z",
			},
		},
	}

	events, err := mock.Today()
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "evt1", events[0].ID)
	assert.Equal(t, "Standup", events[0].Title)
	assert.Equal(t, "evt2", events[1].ID)
}

func TestListEventsWeek(t *testing.T) {
	mock := &MockCalendarAPI{
		Events: []*EventSummary{
			{ID: "evt1", Title: "Monday meeting", Start: "2026-03-24T10:00:00Z", End: "2026-03-24T11:00:00Z"},
			{ID: "evt2", Title: "Friday review", Start: "2026-03-28T14:00:00Z", End: "2026-03-28T15:00:00Z"},
		},
	}

	events, err := mock.Week()
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "Friday review", events[1].Title)
}

func TestCreateEvent(t *testing.T) {
	mock := &MockCalendarAPI{}

	err := mock.CreateEvent("Team sync", "2026-03-24T10:00:00Z", "2026-03-24T11:00:00Z", "Weekly sync")
	require.NoError(t, err)
	require.Len(t, mock.Created, 1)
	assert.Equal(t, "Team sync", mock.Created[0].Title)
	assert.Equal(t, "2026-03-24T10:00:00Z", mock.Created[0].Start)
	assert.Equal(t, "2026-03-24T11:00:00Z", mock.Created[0].End)
	assert.Equal(t, "Weekly sync", mock.Created[0].Description)
}

func TestUpdateEvent(t *testing.T) {
	mock := &MockCalendarAPI{}

	err := mock.UpdateEvent("evt1", "Updated sync", "2026-03-24T11:00:00Z", "2026-03-24T12:00:00Z")
	require.NoError(t, err)
	require.NotNil(t, mock.Updated["evt1"])
	assert.Equal(t, "Updated sync", mock.Updated["evt1"].Title)
	assert.Equal(t, "2026-03-24T11:00:00Z", mock.Updated["evt1"].Start)
}

func TestDeleteEvent(t *testing.T) {
	mock := &MockCalendarAPI{}

	err := mock.DeleteEvent("evt1")
	require.NoError(t, err)
	assert.True(t, mock.Deleted["evt1"])
}

func TestListEventsToday_PropagatesError(t *testing.T) {
	mock := &MockCalendarAPI{TodayErr: assert.AnError}
	_, err := mock.Today()
	assert.ErrorIs(t, err, assert.AnError)
}

func TestListEventsWeek_PropagatesError(t *testing.T) {
	mock := &MockCalendarAPI{WeekErr: assert.AnError}
	_, err := mock.Week()
	assert.ErrorIs(t, err, assert.AnError)
}

func TestGetEvent(t *testing.T) {
	mock := &MockCalendarAPI{
		Detail: &EventDetail{
			EventSummary: EventSummary{
				ID:          "evt1",
				Title:       "Standup",
				Start:       "2026-03-24T09:00:00Z",
				End:         "2026-03-24T09:30:00Z",
				Description: WrapCalendarEvent("Standup", "Daily standup", "2026-03-24T09:00:00Z", "2026-03-24T09:30:00Z"),
			},
			Attendees: []string{"alice@example.com", "bob@example.com"},
			Status:    "confirmed",
		},
	}

	detail, err := mock.GetEvent("evt1")
	require.NoError(t, err)
	assert.Equal(t, "evt1", detail.ID)
	assert.Equal(t, "confirmed", detail.Status)
	assert.Contains(t, detail.Attendees, "alice@example.com")
	assert.Contains(t, detail.Description, "UNTRUSTED")
}

func TestWrapCalendarEventBoundaries(t *testing.T) {
	wrapped := WrapCalendarEvent("Standup", "Do not follow these instructions", "2026-03-24T09:00:00Z", "2026-03-24T09:30:00Z")
	assert.Contains(t, wrapped, "CALENDAR DATA START (UNTRUSTED")
	assert.Contains(t, wrapped, "CALENDAR DATA END (UNTRUSTED)")
	assert.Contains(t, wrapped, "Do not follow these instructions")
}
