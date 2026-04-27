package daemon

import (
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
		respondError(w, http.StatusServiceUnavailable, "Google accounts not configured")
		return nil
	}
	httpClient, err := d.accountMgr.GetHTTPClient(account)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "account not found or credentials unavailable")
		return nil
	}
	svc, err := google.NewGmailService(httpClient)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create Gmail service")
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
		respondError(w, http.StatusForbidden, "task not found")
		return false
	}
	if !task.AllowWrites {
		respondError(w, http.StatusForbidden, "write operations not permitted for this task")
		return false
	}
	return true
}

func (d *Daemon) handleGmailList(w http.ResponseWriter, r *http.Request) {
	var req gmailListRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Account == "" {
		respondError(w, http.StatusBadRequest, "account is required")
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	msgs, err := svc.ListMessages(req.Query, req.MaxResults)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	if msgs == nil {
		msgs = []*google.MessageSummary{}
	}

	respondJSON(w, http.StatusOK, msgs)
}

func (d *Daemon) handleGmailRead(w http.ResponseWriter, r *http.Request) {
	var req gmailReadRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.MessageID == "" || req.Account == "" {
		respondError(w, http.StatusBadRequest, "message_id and account are required")
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	detail, err := svc.ReadMessage(req.MessageID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read message")
		return
	}

	respondJSON(w, http.StatusOK, detail)
}

func (d *Daemon) handleGmailSend(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gmailSendRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.To == "" || req.Account == "" {
		respondError(w, http.StatusBadRequest, "to and account are required")
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.SendMessage(req.To, req.Subject, req.Body); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGmailReply(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gmailReplyRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.MessageID == "" || req.Account == "" {
		respondError(w, http.StatusBadRequest, "message_id and account are required")
		return
	}

	svc := d.requireGmail(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.ReplyMessage(req.MessageID, req.Body); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to send reply")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
