package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrWriteNotPermitted is returned when the daemon rejects a write because
// the submitting task lacks allow_writes. The message text is preserved
// verbatim from the pre-helper Gmail/Gcal call sites.
var ErrWriteNotPermitted = errors.New("write not permitted: task does not have allow_writes")

// ErrNotFound is returned when the daemon responds 404 for a single-resource
// lookup. Callers wrap this with their per-resource message via errors.Is.
var ErrNotFound = errors.New("not found")

// postJSON marshals body to JSON, POSTs it to path with Content-Type:
// application/json, and returns the response body bytes on expectedStatus.
//
// When taskID is non-empty, X-Gobrrr-Task-ID is set so the daemon can
// authorize the call against the originating task. When body is nil, the
// request is sent with no body (used for parameterless POSTs).
//
// Status mapping:
//   - 403 → ErrWriteNotPermitted (preserves the pre-helper error text).
//   - expectedStatus → returns response body bytes.
//   - other → fmt.Errorf("unexpected status %d from POST %s", code, path).
func (c *Client) postJSON(path string, body any, taskID string, expectedStatus int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrWriteNotPermitted
	}
	if resp.StatusCode != expectedStatus {
		return nil, fmt.Errorf("unexpected status %d from POST %s", resp.StatusCode, path)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return raw, nil
}

// getJSON GETs path. Returns response body bytes on 200 OK; ErrNotFound on
// 404; "unexpected status" error otherwise.
func (c *Client) getJSON(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET %s", resp.StatusCode, path)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return raw, nil
}

// deleteResource issues DELETE path. Returns nil on expectedStatus,
// ErrNotFound on 404, "unexpected status" error otherwise.
func (c *Client) deleteResource(path string, expectedStatus int) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected status %d from DELETE %s", resp.StatusCode, path)
	}
	return nil
}
