package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
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
		if err := decodeJSON(r, &body); err != nil {
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

// approvalStreamHandler builds the SSE handler for GET /approvals/stream.
// It rehydrates pending approvals on connect (so a late subscriber, e.g. a
// restarted bot, catches up) then switches to the hub's live fan-out.
func approvalStreamHandler(d *ApprovalDispatcher, hub *ApprovalHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		// Rehydration pass. A connection that subscribes between store.Save
		// and the onCreate callback in ApprovalDispatcher.Create may see the
		// same approval here and in the live channel — subscribers MUST dedupe
		// by Request.ID.
		pending, _ := d.List()
		for _, req := range pending {
			ev := ApprovalEvent{Type: ApprovalEventCreated, Request: req}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		for {
			select {
			case event, open := <-ch:
				if !open {
					return
				}
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
