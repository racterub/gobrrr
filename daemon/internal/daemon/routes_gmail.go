package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/racterub/gobrrr/internal/google"
)

// gmailListRequest is the JSON body for POST /gmail/list.
type gmailListRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Account    string `json:"account"`
}

// gmailReadRequest is the JSON body for POST /gmail/read.
type gmailReadRequest struct {
	MessageID string `json:"message_id"`
	Account   string `json:"account"`
}

// gmailSendRequest is the JSON body for POST /gmail/send.
type gmailSendRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Account string `json:"account"`
}

// gmailReplyRequest is the JSON body for POST /gmail/reply.
type gmailReplyRequest struct {
	MessageID string `json:"message_id"`
	Body      string `json:"body"`
	Account   string `json:"account"`
}

// requireGmail returns a GmailAPI for the given account name. It writes an
// HTTP error response and returns nil if the account manager is not configured
// or the Gmail service cannot be created.
func (d *Daemon) requireGmail(w http.ResponseWriter, account string) google.GmailAPI {
	if d.accountMgr == nil {
		http.Error(w, `{"error":"Google accounts not configured"}`, http.StatusServiceUnavailable)
		return nil
	}
	httpClient, err := d.accountMgr.GetHTTPClient(account)
	if err != nil {
		http.Error(w, `{"error":"account not found or credentials unavailable"}`, http.StatusServiceUnavailable)
		return nil
	}
	svc, err := google.NewGmailService(httpClient)
	if err != nil {
		http.Error(w, `{"error":"failed to create Gmail service"}`, http.StatusInternalServerError)
		return nil
	}
	return svc
}

// checkWritePermission returns false and writes a 403 response if the request
// carries an X-Gobrrr-Task-ID header whose task has AllowWrites=false.
// If no header is present (direct CLI call), writes are allowed.
func (d *Daemon) checkWritePermission(w http.ResponseWriter, r *http.Request) bool {
	taskID := r.Header.Get("X-Gobrrr-Task-ID")
	if taskID == "" {
		// Direct CLI invocation — allow.
		return true
	}
	task, err := d.queue.Get(taskID)
	if err != nil {
		// Unknown task ID — deny to be safe.
		http.Error(w, `{"error":"task not found"}`, http.StatusForbidden)
		return false
	}
	if !task.AllowWrites {
		http.Error(w, `{"error":"write operations not permitted for this task"}`, http.StatusForbidden)
		return false
	}
	return true
}

func (d *Daemon) handleGmailList(w http.ResponseWriter, r *http.Request) {
	var req gmailListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Account == "" {
		http.Error(w, `{"error":"account is required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	msgs, err := svc.ListMessages(req.Query, req.MaxResults)
	if err != nil {
		http.Error(w, `{"error":"failed to list messages"}`, http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []*google.MessageSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs) //nolint:errcheck
}

func (d *Daemon) handleGmailRead(w http.ResponseWriter, r *http.Request) {
	var req gmailReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.MessageID == "" || req.Account == "" {
		http.Error(w, `{"error":"message_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	detail, err := svc.ReadMessage(req.MessageID)
	if err != nil {
		http.Error(w, `{"error":"failed to read message"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail) //nolint:errcheck
}

func (d *Daemon) handleGmailSend(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gmailSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.To == "" || req.Account == "" {
		http.Error(w, `{"error":"to and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.SendMessage(req.To, req.Subject, req.Body); err != nil {
		http.Error(w, `{"error":"failed to send message"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGmailReply(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gmailReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.MessageID == "" || req.Account == "" {
		http.Error(w, `{"error":"message_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.ReplyMessage(req.MessageID, req.Body); err != nil {
		http.Error(w, `{"error":"failed to send reply"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
