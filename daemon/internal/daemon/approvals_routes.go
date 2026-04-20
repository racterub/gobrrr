package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
)

// approvalDecisionHandler builds the handler for POST /approvals/{id}. It
// expects a JSON body of the form {"decision": "<action>"} where <action> is
// one of the kind's Actions (e.g. "approve", "deny", "skip_binary").
func approvalDecisionHandler(d *ApprovalDispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Decision string `json:"decision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if body.Decision == "" {
			http.Error(w, "missing decision", http.StatusBadRequest)
			return
		}
		if err := d.Decide(id, body.Decision); err != nil {
			if errors.Is(err, ErrApprovalNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
