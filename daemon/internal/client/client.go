// Package client provides an HTTP client for the gobrrr daemon Unix socket API.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/racterub/gobrrr/internal/memory"
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

// --- Task methods ---

// SubmitTask submits a new task to the daemon and returns the created Task.
func (c *Client) SubmitTask(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int, warm bool) (*daemon.Task, error) {
	body := submitTaskRequest{
		Prompt:      prompt,
		ReplyTo:     replyTo,
		Priority:    priority,
		AllowWrites: allowWrites,
		TimeoutSec:  timeoutSec,
		Warm:        warm,
	}
	raw, err := c.postJSON("/tasks", body, "", http.StatusCreated)
	if err != nil {
		return nil, err
	}
	var task daemon.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}

// ListTasks returns all tasks. When all is true, completed/failed tasks are included.
func (c *Client) ListTasks(all bool) ([]*daemon.Task, error) {
	path := "/tasks"
	if all {
		path += "?all=true"
	}
	raw, err := c.getJSON(path)
	if err != nil {
		return nil, err
	}
	var tasks []*daemon.Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return tasks, nil
}

// GetTask returns the task with the given ID.
func (c *Client) GetTask(id string) (*daemon.Task, error) {
	raw, err := c.getJSON("/tasks/" + id)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var task daemon.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}

// CancelTask cancels the task with the given ID.
func (c *Client) CancelTask(id string) error {
	err := c.deleteResource("/tasks/"+id, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("task %q not found", id)
	}
	return err
}

// GetLogs returns the log content for the task with the given ID.
func (c *Client) GetLogs(id string) (string, error) {
	raw, err := c.getJSON("/tasks/" + id + "/logs")
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("logs for task %q not found", id)
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// --- Memory methods ---

// SaveMemory saves a new memory entry via the daemon.
func (c *Client) SaveMemory(content string, tags []string, source string) (*memory.Entry, error) {
	body := saveMemoryRequest{Content: content, Tags: tags, Source: source}
	raw, err := c.postJSON("/memory", body, "", http.StatusCreated)
	if err != nil {
		return nil, err
	}
	var entry memory.Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &entry, nil
}

// SearchMemory searches memory entries. Pass empty query/tags for listing.
func (c *Client) SearchMemory(query string, tags []string, limit int) ([]*memory.Entry, error) {
	u, _ := url.Parse(c.baseURL + "/memory")
	q := u.Query()
	if query != "" {
		q.Set("q", query)
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()
	raw, err := c.getJSON(strings.TrimPrefix(u.String(), c.baseURL))
	if err != nil {
		return nil, err
	}
	var entries []*memory.Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return entries, nil
}

// GetMemory returns a single memory entry by ID.
func (c *Client) GetMemory(id string) (*memory.Entry, error) {
	raw, err := c.getJSON("/memory/" + id)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var entry memory.Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &entry, nil
}

// DeleteMemory deletes a memory entry by ID.
func (c *Client) DeleteMemory(id string) error {
	err := c.deleteResource("/memory/"+id, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("memory %q not found", id)
	}
	return err
}

// --- Gmail methods ---

// GmailList fetches messages matching query for the given account.
// taskID, if non-empty, is sent as X-Gobrrr-Task-ID. Returns raw JSON.
func (c *Client) GmailList(query string, maxResults int, account, taskID string) (string, error) {
	body := gmailListRequest{Query: query, MaxResults: maxResults, Account: account}
	raw, err := c.postJSON("/gmail/list", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GmailRead fetches the full content of a message by ID.
// taskID, if non-empty, is sent as X-Gobrrr-Task-ID. Returns raw JSON.
func (c *Client) GmailRead(messageID, account, taskID string) (string, error) {
	body := gmailReadRequest{MessageID: messageID, Account: account}
	raw, err := c.postJSON("/gmail/read", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GmailSend sends an email. taskID, if non-empty, is sent as X-Gobrrr-Task-ID
// so the daemon can enforce write authorization.
func (c *Client) GmailSend(to, subject, body, account, taskID string) error {
	reqBody := gmailSendRequest{To: to, Subject: subject, Body: body, Account: account}
	_, err := c.postJSON("/gmail/send", reqBody, taskID, http.StatusNoContent)
	return err
}

// GmailReply sends a reply to a message. taskID, if non-empty, is sent as
// X-Gobrrr-Task-ID so the daemon can enforce write authorization.
func (c *Client) GmailReply(messageID, body, account, taskID string) error {
	reqBody := gmailReplyRequest{MessageID: messageID, Body: body, Account: account}
	_, err := c.postJSON("/gmail/reply", reqBody, taskID, http.StatusNoContent)
	return err
}

// --- Calendar methods ---

// GcalToday fetches today's calendar events.
// taskID, if non-empty, is sent as X-Gobrrr-Task-ID. Returns raw JSON.
func (c *Client) GcalToday(account, taskID string) (string, error) {
	body := gcalAccountRequest{Account: account}
	raw, err := c.postJSON("/gcal/today", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GcalWeek fetches this week's calendar events.
// taskID, if non-empty, is sent as X-Gobrrr-Task-ID. Returns raw JSON.
func (c *Client) GcalWeek(account, taskID string) (string, error) {
	body := gcalAccountRequest{Account: account}
	raw, err := c.postJSON("/gcal/week", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GcalGetEvent fetches a single calendar event by ID.
// taskID, if non-empty, is sent as X-Gobrrr-Task-ID. Returns raw JSON.
func (c *Client) GcalGetEvent(eventID, account, taskID string) (string, error) {
	body := gcalGetRequest{EventID: eventID, Account: account}
	raw, err := c.postJSON("/gcal/get", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GcalCreateEvent creates a new calendar event. taskID, if non-empty, is sent
// as X-Gobrrr-Task-ID so the daemon can enforce write authorization.
func (c *Client) GcalCreateEvent(title, start, end, description, account, taskID string) error {
	reqBody := gcalCreateRequest{Title: title, Start: start, End: end, Description: description, Account: account}
	_, err := c.postJSON("/gcal/create", reqBody, taskID, http.StatusNoContent)
	return err
}

// GcalUpdateEvent updates an existing calendar event. taskID, if non-empty, is
// sent as X-Gobrrr-Task-ID so the daemon can enforce write authorization.
func (c *Client) GcalUpdateEvent(eventID, title, start, end, account, taskID string) error {
	reqBody := gcalUpdateRequest{EventID: eventID, Title: title, Start: start, End: end, Account: account}
	_, err := c.postJSON("/gcal/update", reqBody, taskID, http.StatusNoContent)
	return err
}

// GcalDeleteEvent deletes a calendar event. taskID, if non-empty, is sent as
// X-Gobrrr-Task-ID so the daemon can enforce write authorization.
func (c *Client) GcalDeleteEvent(eventID, account, taskID string) error {
	reqBody := gcalDeleteRequest{EventID: eventID, Account: account}
	_, err := c.postJSON("/gcal/delete", reqBody, taskID, http.StatusNoContent)
	return err
}

// --- Session methods ---

// SessionStatus returns the current session status.
func (c *Client) SessionStatus() (map[string]any, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/session/status")
	if err != nil {
		return nil, fmt.Errorf("GET /session/status: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// SessionStart starts the Telegram session.
func (c *Client) SessionStart() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/start", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/start: %s", string(body))
	}
	return nil
}

// SessionStop stops the Telegram session.
func (c *Client) SessionStop() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/stop", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/stop: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/stop: %s", string(body))
	}
	return nil
}

// SessionRestart restarts the Telegram session.
func (c *Client) SessionRestart() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/restart", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/restart: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/restart: %s", string(body))
	}
	return nil
}

// --- Scheduler methods ---

// CreateSchedule creates a new recurring schedule.
func (c *Client) CreateSchedule(name, cronExpr, prompt, replyTo string, allowWrites bool) (map[string]any, error) {
	body := struct {
		Name        string `json:"name"`
		Cron        string `json:"cron"`
		Prompt      string `json:"prompt"`
		ReplyTo     string `json:"reply_to"`
		AllowWrites bool   `json:"allow_writes"`
	}{name, cronExpr, prompt, replyTo, allowWrites}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}
	resp, err := c.httpClient.Post(c.baseURL+"/schedules", "application/json", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("POST /schedules: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST /schedules: %s", string(respBody))
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// ListSchedules returns all scheduled tasks.
func (c *Client) ListSchedules() ([]map[string]any, error) {
	raw, err := c.getJSON("/schedules")
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// RemoveSchedule removes a schedule by name.
func (c *Client) RemoveSchedule(name string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/schedules/"+name, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /schedules/%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE /schedules/%s: %s", name, string(body))
	}
	return nil
}

// Health returns the daemon health information.
func (c *Client) Health() (map[string]interface{}, error) {
	raw, err := c.getJSON("/health")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}
