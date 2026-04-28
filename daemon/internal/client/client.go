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
	"net/url"
	"strings"
	"time"

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

// submitTaskRequest mirrors the daemon's expected POST /tasks body.
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	Warm        bool   `json:"warm"`
}

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

// --- memory methods ---

type saveMemoryRequest struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Source  string   `json:"source"`
}

// SaveMemory saves a new memory entry via the daemon.
func (c *Client) SaveMemory(content string, tags []string, source string) (*memory.Entry, error) {
	body := saveMemoryRequest{Content: content, Tags: tags, Source: source}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/memory", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST /memory: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status %d from POST /memory", resp.StatusCode)
	}

	var entry memory.Entry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
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

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("GET /memory: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET /memory", resp.StatusCode)
	}

	var entries []*memory.Entry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return entries, nil
}

// GetMemory returns a single memory entry by ID.
func (c *Client) GetMemory(id string) (*memory.Entry, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/memory/" + id)
	if err != nil {
		return nil, fmt.Errorf("GET /memory/%s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET /memory/%s", resp.StatusCode, id)
	}

	var entry memory.Entry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &entry, nil
}

// DeleteMemory deletes a memory entry by ID.
func (c *Client) DeleteMemory(id string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/memory/"+id, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /memory/%s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("memory %q not found", id)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from DELETE /memory/%s", resp.StatusCode, id)
	}
	return nil
}

// --- Gmail methods ---

// gmailListRequest mirrors the daemon's POST /gmail/list body.
type gmailListRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Account    string `json:"account"`
}

// gmailReadRequest mirrors the daemon's POST /gmail/read body.
type gmailReadRequest struct {
	MessageID string `json:"message_id"`
	Account   string `json:"account"`
}

// gmailSendRequest mirrors the daemon's POST /gmail/send body.
type gmailSendRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Account string `json:"account"`
}

// gmailReplyRequest mirrors the daemon's POST /gmail/reply body.
type gmailReplyRequest struct {
	MessageID string `json:"message_id"`
	Body      string `json:"body"`
	Account   string `json:"account"`
}

// GmailList fetches a list of messages matching query for the given account.
// If taskID is non-empty it is sent as the X-Gobrrr-Task-ID header so the
// daemon can attribute the call to a specific task. It returns the raw JSON
// response body as a string.
func (c *Client) GmailList(query string, maxResults int, account, taskID string) (string, error) {
	body := gmailListRequest{Query: query, MaxResults: maxResults, Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/list", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gmail/list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gmail/list", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}

// GmailRead fetches the full content of a message by ID for the given account.
// If taskID is non-empty it is sent as the X-Gobrrr-Task-ID header. It returns
// the raw JSON response body as a string.
func (c *Client) GmailRead(messageID, account, taskID string) (string, error) {
	body := gmailReadRequest{MessageID: messageID, Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/read", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gmail/read: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gmail/read", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}

// GmailSend sends an email via the daemon. If taskID is non-empty it is sent
// as the X-Gobrrr-Task-ID header so the daemon can enforce write authorization.
func (c *Client) GmailSend(to, subject, body, account, taskID string) error {
	reqBody := gmailSendRequest{To: to, Subject: subject, Body: body, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/send", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gmail/send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gmail/send", resp.StatusCode)
	}
	return nil
}

// GmailReply sends a reply to the given message via the daemon. If taskID is
// non-empty it is sent as the X-Gobrrr-Task-ID header so the daemon can
// enforce write authorization.
func (c *Client) GmailReply(messageID, body, account, taskID string) error {
	reqBody := gmailReplyRequest{MessageID: messageID, Body: body, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/reply", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gmail/reply: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gmail/reply", resp.StatusCode)
	}
	return nil
}

// --- Calendar methods ---

// gcalAccountRequest mirrors the daemon's gcal endpoints that only need account.
type gcalAccountRequest struct {
	Account string `json:"account"`
}

// gcalGetRequest mirrors the daemon's POST /gcal/get body.
type gcalGetRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// gcalCreateRequest mirrors the daemon's POST /gcal/create body.
type gcalCreateRequest struct {
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
	Account     string `json:"account"`
}

// gcalUpdateRequest mirrors the daemon's POST /gcal/update body.
type gcalUpdateRequest struct {
	EventID string `json:"event_id"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Account string `json:"account"`
}

// gcalDeleteRequest mirrors the daemon's POST /gcal/delete body.
type gcalDeleteRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// GcalToday fetches today's calendar events for the given account. If taskID
// is non-empty it is sent as the X-Gobrrr-Task-ID header. It returns the raw
// JSON response body as a string.
func (c *Client) GcalToday(account, taskID string) (string, error) {
	body := gcalAccountRequest{Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/today", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gcal/today: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gcal/today", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}

// GcalWeek fetches this week's calendar events for the given account. If
// taskID is non-empty it is sent as the X-Gobrrr-Task-ID header. It returns
// the raw JSON response body as a string.
func (c *Client) GcalWeek(account, taskID string) (string, error) {
	body := gcalAccountRequest{Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/week", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gcal/week: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gcal/week", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}

// GcalGetEvent fetches a single calendar event by ID for the given account.
// If taskID is non-empty it is sent as the X-Gobrrr-Task-ID header. It returns
// the raw JSON response body as a string.
func (c *Client) GcalGetEvent(eventID, account, taskID string) (string, error) {
	body := gcalGetRequest{EventID: eventID, Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/get", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gcal/get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gcal/get", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}

// GcalCreateEvent creates a new calendar event via the daemon. If taskID is
// non-empty it is sent as the X-Gobrrr-Task-ID header so the daemon can
// enforce write authorization.
func (c *Client) GcalCreateEvent(title, start, end, description, account, taskID string) error {
	reqBody := gcalCreateRequest{Title: title, Start: start, End: end, Description: description, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/create", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gcal/create: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gcal/create", resp.StatusCode)
	}
	return nil
}

// GcalUpdateEvent updates an existing calendar event via the daemon. If taskID
// is non-empty it is sent as the X-Gobrrr-Task-ID header so the daemon can
// enforce write authorization.
func (c *Client) GcalUpdateEvent(eventID, title, start, end, account, taskID string) error {
	reqBody := gcalUpdateRequest{EventID: eventID, Title: title, Start: start, End: end, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/update", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gcal/update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gcal/update", resp.StatusCode)
	}
	return nil
}

// GcalDeleteEvent deletes a calendar event via the daemon. If taskID is
// non-empty it is sent as the X-Gobrrr-Task-ID header so the daemon can
// enforce write authorization.
func (c *Client) GcalDeleteEvent(eventID, account, taskID string) error {
	reqBody := gcalDeleteRequest{EventID: eventID, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gcal/delete", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gcal/delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gcal/delete", resp.StatusCode)
	}
	return nil
}

// WaitForTask polls GetTask every 2 seconds until the task reaches a terminal
// state (completed, failed, or cancelled). It returns the result string on
// success, an error on failure/cancellation, or a descriptive error if the
// daemon connection is lost.
func (c *Client) WaitForTask(taskID string) (string, error) {
	for {
		task, err := c.GetTask(taskID)
		if err != nil {
			return "", fmt.Errorf("daemon connection lost, result will be in ~/.gobrrr/logs/%s.log", taskID)
		}
		switch task.Status {
		case "completed":
			if task.Result != nil {
				return *task.Result, nil
			}
			return "", nil
		case "failed":
			errMsg := "unknown error"
			if task.Error != nil {
				errMsg = *task.Error
			}
			return "", fmt.Errorf("task failed: %s", errMsg)
		case "cancelled":
			return "", fmt.Errorf("task was cancelled")
		}
		time.Sleep(500 * time.Millisecond)
	}
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

	resp, err := c.httpClient.Post(c.baseURL+"/schedules", "application/json", bytes.NewReader(data))
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
	resp, err := c.httpClient.Get(c.baseURL + "/schedules")
	if err != nil {
		return nil, fmt.Errorf("GET /schedules: %w", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
