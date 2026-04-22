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

	"github.com/racterub/gobrrr/internal/clawhub"
	"github.com/racterub/gobrrr/internal/skills"
)

// ListSkills returns all installed skills known to the daemon.
func (c *Client) ListSkills() ([]skills.Skill, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/skills")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list skills: %s: %s", resp.Status, string(body))
	}
	var out []skills.Skill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchSkills queries the daemon's ClawHub proxy.
func (c *Client) SearchSkills(q string) ([]clawhub.SkillSummary, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/skills/search?q=" + url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search skills: %s: %s", resp.Status, string(body))
	}
	var out []clawhub.SkillSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// InstallResult is what the daemon returns after staging an install.
type InstallResult struct {
	RequestID string                 `json:"request_id"`
	Request   clawhub.InstallRequest `json:"request"`
}

// InstallSkill asks the daemon to fetch & stage <slug>[@version]; version="" for latest.
func (c *Client) InstallSkill(slug, version string) (*InstallResult, error) {
	body, _ := json.Marshal(map[string]string{"slug": slug, "version": version})
	resp, err := c.httpClient.Post(c.baseURL+"/skills/install", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("install skill: %s: %s", resp.Status, string(b))
	}
	var out InstallResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UninstallSkill removes an installed skill from ~/.gobrrr/skills/<slug>.
func (c *Client) UninstallSkill(slug string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/skills/"+url.PathEscape(slug), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("uninstall skill: %s: %s", resp.Status, string(b))
	}
	return nil
}

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

// ApprovalEvent mirrors daemon.ApprovalEvent for SSE consumers.
type ApprovalEvent struct {
	Type     string           `json:"type"`
	Request  *ApprovalRequest `json:"request,omitempty"`
	ID       string           `json:"id,omitempty"`
	Decision string           `json:"decision,omitempty"`
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
