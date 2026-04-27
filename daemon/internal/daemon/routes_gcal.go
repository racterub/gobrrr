package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/racterub/gobrrr/internal/google"
)

// gcalAccountRequest is the JSON body for calendar endpoints that only need account.
type gcalAccountRequest struct {
	Account string `json:"account"`
}

// gcalGetRequest is the JSON body for POST /gcal/get.
type gcalGetRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// gcalCreateRequest is the JSON body for POST /gcal/create.
type gcalCreateRequest struct {
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
	Account     string `json:"account"`
}

// gcalUpdateRequest is the JSON body for POST /gcal/update.
type gcalUpdateRequest struct {
	EventID string `json:"event_id"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Account string `json:"account"`
}

// gcalDeleteRequest is the JSON body for POST /gcal/delete.
type gcalDeleteRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// requireCalendar returns a CalendarAPI for the given account name. It writes
// an HTTP error response and returns nil if the account manager is not
// configured or the Calendar service cannot be created.
func (d *Daemon) requireCalendar(w http.ResponseWriter, account string) google.CalendarAPI {
	if d.accountMgr == nil {
		http.Error(w, `{"error":"Google accounts not configured"}`, http.StatusServiceUnavailable)
		return nil
	}
	httpClient, err := d.accountMgr.GetHTTPClient(account)
	if err != nil {
		http.Error(w, `{"error":"account not found or credentials unavailable"}`, http.StatusServiceUnavailable)
		return nil
	}
	svc, err := google.NewCalendarService(httpClient)
	if err != nil {
		http.Error(w, `{"error":"failed to create Calendar service"}`, http.StatusInternalServerError)
		return nil
	}
	return svc
}

func (d *Daemon) handleGcalToday(w http.ResponseWriter, r *http.Request) {
	var req gcalAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Account == "" {
		http.Error(w, `{"error":"account is required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	events, err := svc.Today()
	if err != nil {
		http.Error(w, `{"error":"failed to list today's events"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*google.EventSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events) //nolint:errcheck
}

func (d *Daemon) handleGcalWeek(w http.ResponseWriter, r *http.Request) {
	var req gcalAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Account == "" {
		http.Error(w, `{"error":"account is required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	events, err := svc.Week()
	if err != nil {
		http.Error(w, `{"error":"failed to list week's events"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*google.EventSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events) //nolint:errcheck
}

func (d *Daemon) handleGcalGet(w http.ResponseWriter, r *http.Request) {
	var req gcalGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	detail, err := svc.GetEvent(req.EventID)
	if err != nil {
		http.Error(w, `{"error":"failed to get event"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail) //nolint:errcheck
}

func (d *Daemon) handleGcalCreate(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.Account == "" {
		http.Error(w, `{"error":"title and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.CreateEvent(req.Title, req.Start, req.End, req.Description); err != nil {
		http.Error(w, `{"error":"failed to create event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGcalUpdate(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.UpdateEvent(req.EventID, req.Title, req.Start, req.End); err != nil {
		http.Error(w, `{"error":"failed to update event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGcalDelete(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.DeleteEvent(req.EventID); err != nil {
		http.Error(w, `{"error":"failed to delete event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
