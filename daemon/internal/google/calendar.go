package google

import (
	"fmt"
	"net/http"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// CalendarAPI is the interface for Google Calendar operations. Implementations
// include CalendarService (real API) and MockCalendarAPI (tests).
type CalendarAPI interface {
	Today() ([]*EventSummary, error)
	Week() ([]*EventSummary, error)
	GetEvent(eventID string) (*EventDetail, error)
	CreateEvent(title, start, end, description string) error
	UpdateEvent(eventID, title, start, end string) error
	DeleteEvent(eventID string) error
}

// EventSummary holds a brief summary of a calendar event.
type EventSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

// EventDetail extends EventSummary with attendees and status.
type EventDetail struct {
	EventSummary
	Attendees []string `json:"attendees"`
	Status    string   `json:"status"`
}

// CalendarService implements CalendarAPI using the real Google Calendar API.
type CalendarService struct {
	svc *calendar.Service
}

// NewCalendarService creates a CalendarService backed by the provided HTTP client.
func NewCalendarService(client *http.Client) (*CalendarService, error) {
	svc, err := calendar.NewService(nil, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("calendar: create service: %w", err)
	}
	return &CalendarService{svc: svc}, nil
}

// Today returns all calendar events for today. Event title and description are
// wrapped with UNTRUSTED boundaries.
func (c *CalendarService) Today() ([]*EventSummary, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	return c.listEvents(startOfDay, endOfDay)
}

// Week returns all calendar events for the current week (next 7 days). Event
// title and description are wrapped with UNTRUSTED boundaries.
func (c *CalendarService) Week() ([]*EventSummary, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfWeek := startOfDay.Add(7 * 24 * time.Hour)
	return c.listEvents(startOfDay, endOfWeek)
}

// listEvents fetches events within the given time range and wraps them with
// UNTRUSTED boundaries.
func (c *CalendarService) listEvents(from, to time.Time) ([]*EventSummary, error) {
	var result []*EventSummary
	err := WithRetry(func() error {
		resp, err := c.svc.Events.List("primary").
			TimeMin(from.Format(time.RFC3339)).
			TimeMax(to.Format(time.RFC3339)).
			SingleEvents(true).
			OrderBy("startTime").
			Do()
		if err != nil {
			return err
		}

		summaries := make([]*EventSummary, 0, len(resp.Items))
		for _, item := range resp.Items {
			s := eventToSummary(item)
			// Wrap description with UNTRUSTED boundaries to prevent prompt injection.
			s.Description = WrapCalendarEvent(s.Title, s.Description, s.Start, s.End)
			summaries = append(summaries, s)
		}
		result = summaries
		return nil
	})
	return result, err
}

// GetEvent fetches the full detail of the event with the given ID.
func (c *CalendarService) GetEvent(eventID string) (*EventDetail, error) {
	var detail *EventDetail
	err := WithRetry(func() error {
		item, err := c.svc.Events.Get("primary", eventID).Do()
		if err != nil {
			return err
		}

		s := eventToSummary(item)
		d := &EventDetail{
			EventSummary: *s,
			Status:       item.Status,
		}
		for _, a := range item.Attendees {
			d.Attendees = append(d.Attendees, a.Email)
		}
		d.Description = WrapCalendarEvent(d.Title, d.Description, d.Start, d.End)
		detail = d
		return nil
	})
	return detail, err
}

// CreateEvent creates a new calendar event.
func (c *CalendarService) CreateEvent(title, start, end, description string) error {
	return WithRetry(func() error {
		event := &calendar.Event{
			Summary:     title,
			Description: description,
			Start:       &calendar.EventDateTime{DateTime: start, TimeZone: "UTC"},
			End:         &calendar.EventDateTime{DateTime: end, TimeZone: "UTC"},
		}
		_, err := c.svc.Events.Insert("primary", event).Do()
		return err
	})
}

// UpdateEvent updates the title, start, and end of the event with the given ID.
func (c *CalendarService) UpdateEvent(eventID, title, start, end string) error {
	return WithRetry(func() error {
		item, err := c.svc.Events.Get("primary", eventID).Do()
		if err != nil {
			return err
		}
		if title != "" {
			item.Summary = title
		}
		if start != "" {
			item.Start = &calendar.EventDateTime{DateTime: start, TimeZone: "UTC"}
		}
		if end != "" {
			item.End = &calendar.EventDateTime{DateTime: end, TimeZone: "UTC"}
		}
		_, err = c.svc.Events.Update("primary", eventID, item).Do()
		return err
	})
}

// DeleteEvent deletes the event with the given ID.
func (c *CalendarService) DeleteEvent(eventID string) error {
	return WithRetry(func() error {
		return c.svc.Events.Delete("primary", eventID).Do()
	})
}

// eventToSummary converts a Google Calendar Event to an EventSummary.
func eventToSummary(item *calendar.Event) *EventSummary {
	s := &EventSummary{
		ID:          item.Id,
		Title:       item.Summary,
		Location:    item.Location,
		Description: item.Description,
	}
	if item.Start != nil {
		if item.Start.DateTime != "" {
			s.Start = item.Start.DateTime
		} else {
			s.Start = item.Start.Date
		}
	}
	if item.End != nil {
		if item.End.DateTime != "" {
			s.End = item.End.DateTime
		} else {
			s.End = item.End.Date
		}
	}
	return s
}
