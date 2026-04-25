package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ApprovalRequest mirrors daemon.ApprovalRequest for SSE consumers. Kept
// separate so the client doesn't import the daemon package.
type ApprovalRequest struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Actions   []string        `json:"actions"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
	ExpiresAt string          `json:"expires_at"`
}

// ApprovalEvent mirrors daemon.ApprovalEvent for SSE consumers. Error carries
// the per-kind handler's failure message on "removed" events; empty on success.
type ApprovalEvent struct {
	Type     string           `json:"type"`
	Request  *ApprovalRequest `json:"request,omitempty"`
	ID       string           `json:"id,omitempty"`
	Decision string           `json:"decision,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// DecideApproval posts a decision to the daemon's generic approval endpoint.
// decision must be one of the kind's advertised actions (e.g. "approve",
// "deny", "skip_binary").
func (c *Client) DecideApproval(id, decision string) error {
	body, _ := json.Marshal(map[string]string{"decision": decision})
	resp, err := c.httpClient.Post(c.baseURL+"/approvals/"+url.PathEscape(id),
		"application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("decide approval: %s: %s", resp.Status, string(b))
	}
	return nil
}

// StreamApprovals subscribes to GET /approvals/stream and returns a channel
// of events. The channel is closed when ctx is cancelled or the server ends
// the stream.
func (c *Client) StreamApprovals(ctx context.Context) (<-chan ApprovalEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/approvals/stream", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream approvals: %s", resp.Status)
	}

	out := make(chan ApprovalEvent, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // 1 MiB line cap
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var ev ApprovalEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
