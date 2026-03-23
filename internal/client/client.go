// Package client provides an HTTP client for the gobrrr daemon Unix socket API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/racterub/gobrrr/internal/daemon"
)

// Client communicates with the gobrrr daemon over a Unix socket.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// New creates a Client that connects to the daemon via the given Unix socket path.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	return &Client{
		httpClient: &http.Client{Transport: transport},
		baseURL:    "http://gobrrr",
	}
}

// submitTaskRequest mirrors the daemon's expected POST /tasks body.
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
}

// SubmitTask submits a new task to the daemon and returns the created Task.
func (c *Client) SubmitTask(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int) (*daemon.Task, error) {
	body := submitTaskRequest{
		Prompt:      prompt,
		ReplyTo:     replyTo,
		Priority:    priority,
		AllowWrites: allowWrites,
		TimeoutSec:  timeoutSec,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/tasks", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST /tasks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status %d from POST /tasks", resp.StatusCode)
	}

	var task daemon.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}

// ListTasks returns all tasks. When all is true, completed/failed tasks are
// included.
func (c *Client) ListTasks(all bool) ([]*daemon.Task, error) {
	url := c.baseURL + "/tasks"
	if all {
		url += "?all=true"
	}

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET /tasks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET /tasks", resp.StatusCode)
	}

	var tasks []*daemon.Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return tasks, nil
}

// GetTask returns the task with the given ID.
func (c *Client) GetTask(id string) (*daemon.Task, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/tasks/" + id)
	if err != nil {
		return nil, fmt.Errorf("GET /tasks/%s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET /tasks/%s", resp.StatusCode, id)
	}

	var task daemon.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}

// CancelTask cancels the task with the given ID.
func (c *Client) CancelTask(id string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/tasks/"+id, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /tasks/%s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("task %q not found", id)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from DELETE /tasks/%s", resp.StatusCode, id)
	}
	return nil
}

// GetLogs returns the log content for the task with the given ID.
func (c *Client) GetLogs(id string) (string, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/tasks/" + id + "/logs")
	if err != nil {
		return "", fmt.Errorf("GET /tasks/%s/logs: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("logs for task %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from GET /tasks/%s/logs", resp.StatusCode, id)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(data), nil
}

// Health returns the daemon health information.
func (c *Client) Health() (map[string]interface{}, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return nil, fmt.Errorf("GET /health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET /health", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}
