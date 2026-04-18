// Package daemon implements the gobrrr HTTP daemon that listens on a Unix socket.
package daemon

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	vault "github.com/racterub/gobrrr/internal/crypto"
	"github.com/racterub/gobrrr/internal/google"
	"github.com/racterub/gobrrr/internal/memory"
	"github.com/racterub/gobrrr/internal/scheduler"
	"github.com/racterub/gobrrr/internal/security"
	"github.com/racterub/gobrrr/internal/session"
	"github.com/racterub/gobrrr/internal/telegram"
)

// Daemon is the HTTP daemon that serves the gobrrr API over a Unix socket.
type Daemon struct {
	cfg           *config.Config
	socket        string
	gobrrDir      string
	mux           *http.ServeMux
	queue         *Queue
	workerPool    *WorkerPool
	memStore      *memory.Store
	accountMgr    *google.AccountManager
	notifier      *telegram.Notifier
	sseHub        *SSEHub
	heartbeat     *Heartbeat
	healthChecker *HealthChecker
	confirmGate   *security.Gate
	startTime     time.Time
	session       *session.Manager
	scheduler     *scheduler.Scheduler
	ctx           context.Context
}

// New creates a new Daemon configured to listen on the given socket path.
// gobrrDir is the directory where queue.json and logs are stored.
func New(cfg *config.Config, socket string) *Daemon {
	gobrrDir := config.GobrrDir()
	queuePath := filepath.Join(gobrrDir, "queue.json")

	var q *Queue
	loaded, err := LoadQueue(queuePath)
	if err != nil {
		// If loading fails (e.g. corrupt file), start fresh.
		q = NewQueue(queuePath)
	} else {
		q = loaded
	}

	spawnInterval := time.Duration(cfg.SpawnIntervalSec) * time.Second
	memDir := filepath.Join(gobrrDir, "memory")
	ms := memory.NewStore(memDir)
	wp := NewWorkerPool(q, cfg, cfg.MaxWorkers, spawnInterval, gobrrDir, ms)

	// Initialize AccountManager if the google directory exists and is accessible.
	var acctMgr *google.AccountManager
	googleDir := filepath.Join(gobrrDir, "google")
	if _, err := os.Stat(googleDir); err == nil {
		v, vErr := loadVaultIfAvailable(gobrrDir)
		if vErr == nil && v != nil {
			acctMgr = google.NewAccountManager(googleDir, v)
		}
	}

	// Initialize Telegram notifier if bot token and chat ID are configured.
	// Credentials are stored encrypted by the setup wizard; decrypt before use.
	var notifier *telegram.Notifier
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		v, vErr := loadVaultIfAvailable(gobrrDir)
		if vErr == nil && v != nil {
			tokenBytes, _ := hex.DecodeString(cfg.Telegram.BotToken)
			chatBytes, _ := hex.DecodeString(cfg.Telegram.ChatID)
			decToken, dErr := v.Decrypt(tokenBytes)
			decChat, cErr := v.Decrypt(chatBytes)
			if dErr == nil && cErr == nil {
				notifier = telegram.NewNotifier(string(decToken), string(decChat))
			}
		}
	}

	// Initialize Uptime Kuma heartbeat if configured.
	heartbeatInterval := time.Duration(cfg.UptimeKuma.IntervalSec) * time.Second
	if heartbeatInterval <= 0 {
		heartbeatInterval = 60 * time.Second
	}
	hb := NewHeartbeat(cfg.UptimeKuma.PushURL, heartbeatInterval)
	hc := NewHealthChecker(q)

	d := &Daemon{
		cfg:           cfg,
		socket:        socket,
		gobrrDir:      gobrrDir,
		mux:           http.NewServeMux(),
		queue:         q,
		workerPool:    wp,
		memStore:      ms,
		accountMgr:    acctMgr,
		notifier:      notifier,
		sseHub:        NewSSEHub(),
		heartbeat:     hb,
		healthChecker: hc,
		confirmGate:   security.NewGate(5 * time.Minute),
	}

	// Session manager
	// IMPORTANT: avoid nil-interface trap — a nil *telegram.Notifier passed to
	// an interface parameter creates a non-nil interface that panics on Send().
	if cfg.TelegramSession.Enabled {
		var sessionNotifier session.Notifier
		if notifier != nil {
			sessionNotifier = notifier
		}
		d.session = session.NewManager(cfg.TelegramSession, sessionNotifier)
		d.session.SetWorkDir(cfg.WorkspacePath)
	}

	// Scheduler
	schedulerPath := filepath.Join(gobrrDir, "schedules.json")
	d.scheduler = scheduler.New(schedulerPath, func(prompt, replyTo string, allowWrites bool) error {
		_, err := d.queue.Submit(prompt, replyTo, 5, allowWrites, cfg.DefaultTimeoutSec, false)
		return err
	})
	if err := d.scheduler.Load(); err != nil {
		log.Printf("scheduler: failed to load: %v", err)
	}

	// Wire result routing callback into the worker pool.
	wp.onResult = func(task *Task, result string) {
		if err := d.routeResult(task, result); err != nil {
			log.Printf("routing error for task %s: %v", task.ID, err)
			// Write routing error to task log so user can debug
			logPath := filepath.Join(gobrrDir, "logs", task.ID+".log")
			if f, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); ferr == nil {
				fmt.Fprintf(f, "\n--- ROUTING ERROR ---\n%v\n", err)
				f.Close()
			}
		}
	}
	d.mux.HandleFunc("/health", d.handleHealth)
	d.mux.HandleFunc("POST /tasks", d.handleSubmitTask)
	d.mux.HandleFunc("GET /tasks", d.handleListTasks)
	d.mux.HandleFunc("GET /tasks/{id}", d.handleGetTask)
	d.mux.HandleFunc("DELETE /tasks/{id}", d.handleCancelTask)
	d.mux.HandleFunc("GET /tasks/{id}/logs", d.handleGetTaskLogs)
	d.mux.HandleFunc("POST /tasks/{id}/approve", d.handleApproveTask)
	d.mux.HandleFunc("POST /tasks/{id}/deny", d.handleDenyTask)
	d.mux.HandleFunc("POST /memory", d.handleSaveMemory)
	d.mux.HandleFunc("GET /memory", d.handleSearchMemory)
	d.mux.HandleFunc("GET /memory/{id}", d.handleGetMemory)
	d.mux.HandleFunc("DELETE /memory/{id}", d.handleDeleteMemory)
	d.mux.HandleFunc("POST /gmail/list", d.handleGmailList)
	d.mux.HandleFunc("POST /gmail/read", d.handleGmailRead)
	d.mux.HandleFunc("POST /gmail/send", d.handleGmailSend)
	d.mux.HandleFunc("POST /gmail/reply", d.handleGmailReply)
	d.mux.HandleFunc("POST /gcal/today", d.handleGcalToday)
	d.mux.HandleFunc("POST /gcal/week", d.handleGcalWeek)
	d.mux.HandleFunc("POST /gcal/get", d.handleGcalGet)
	d.mux.HandleFunc("POST /gcal/create", d.handleGcalCreate)
	d.mux.HandleFunc("POST /gcal/update", d.handleGcalUpdate)
	d.mux.HandleFunc("POST /gcal/delete", d.handleGcalDelete)
	d.mux.HandleFunc("GET /tasks/results/stream", d.sseHub.ServeSSE)

	d.mux.HandleFunc("GET /session/status", d.handleSessionStatus)
	d.mux.HandleFunc("POST /session/start", d.handleSessionStart)
	d.mux.HandleFunc("POST /session/stop", d.handleSessionStop)
	d.mux.HandleFunc("POST /session/restart", d.handleSessionRestart)

	d.mux.HandleFunc("POST /schedules", d.handleCreateSchedule)
	d.mux.HandleFunc("GET /schedules", d.handleListSchedules)
	d.mux.HandleFunc("DELETE /schedules/{name}", d.handleRemoveSchedule)

	return d
}

// Run starts the daemon and blocks until ctx is cancelled or a fatal error occurs.
// It returns nil on graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.startTime = time.Now()
	d.ctx = ctx

	// Start systemd watchdog (no-op if NOTIFY_SOCKET is not set).
	go StartWatchdog(ctx)

	// Start warm workers asynchronously — socket must bind immediately, warm pool
	// pre-spawn takes 7-12s per worker which would otherwise delay startup.
	go d.workerPool.StartWarm(ctx)

	// Start the worker pool in the background.
	go d.workerPool.Run(ctx)

	// Start Uptime Kuma heartbeat (no-op if pushURL is empty).
	go d.heartbeat.Run(ctx)

	// Start health monitoring loop that updates heartbeat status every 30 seconds.
	go d.runHealthMonitor(ctx)

	// Start hourly maintenance loop for log and queue pruning.
	go d.runMaintenance(ctx)

	// Start scheduler catch-up and tick loop.
	d.scheduler.CatchUp()
	go d.scheduler.Run(ctx)

	// Start telegram session supervisor.
	if d.session != nil {
		go d.session.Run(ctx)
	}

	// Remove any stale socket file before binding.
	_ = os.Remove(d.socket)

	ln, err := net.Listen("unix", d.socket)
	if err != nil {
		return err
	}

	// Restrict socket to owner read/write only.
	if err := os.Chmod(d.socket, 0600); err != nil {
		ln.Close()
		return err
	}

	srv := &http.Server{Handler: d.mux}

	// Shut down when the context is done.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// runHealthMonitor runs a loop that checks system health every 30 seconds and
// updates the heartbeat with the current status and memory usage.
func (d *Daemon) runHealthMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.updateHeartbeat()
		}
	}
}

// updateHeartbeat checks health and memory usage then calls heartbeat.Update.
func (d *Daemon) updateHeartbeat() {
	healthy, reason := d.healthChecker.Check()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memMB := int(m.Sys / 1024 / 1024)

	status := "up"
	msg := ""
	if !healthy {
		status = "down"
		msg = reason
	} else {
		active := d.queue.List(false)
		running := 0
		queued := 0
		for _, t := range active {
			if t.Status == "running" {
				running++
			} else if t.Status == "queued" {
				queued++
			}
		}
		msg = fmt.Sprintf("%d workers active, %d queued", running, queued)
	}

	d.heartbeat.Update(status, memMB, msg)
}

// warmStatus describes the warm worker pool's capacity and current utilization.
type warmStatus struct {
	Total int `json:"total"`
	Ready int `json:"ready"`
	Busy  int `json:"busy"`
}

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status        string     `json:"status"`
	UptimeSec     int64      `json:"uptime_sec"`
	WorkersActive int        `json:"workers_active"`
	QueueDepth    int        `json:"queue_depth"`
	WarmWorkers   warmStatus `json:"warm_workers"`
}

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeTasks := d.queue.List(false)
	total, ready, busy := d.workerPool.WarmStatus()
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: d.workerPool.Active(),
		QueueDepth:    len(activeTasks),
		WarmWorkers:   warmStatus{Total: total, Ready: ready, Busy: busy},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// submitTaskRequest is the JSON body for POST /tasks.
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	// Warm, when true, requests warm-pool dispatch. This is a hint, not a guarantee —
	// if no warm worker is free, the task falls through to cold spawn.
	Warm bool `json:"warm"`
}

func (d *Daemon) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, `{"error":"prompt is required"}`, http.StatusBadRequest)
		return
	}
	if req.TimeoutSec == 0 {
		req.TimeoutSec = d.cfg.DefaultTimeoutSec
	}

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
	if err != nil {
		http.Error(w, `{"error":"failed to submit task"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task) //nolint:errcheck
}

func (d *Daemon) handleListTasks(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	tasks := d.queue.List(all)
	if tasks == nil {
		tasks = []*Task{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks) //nolint:errcheck
}

func (d *Daemon) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := d.queue.Get(id)
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task) //nolint:errcheck
}

func (d *Daemon) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.queue.Cancel(id); err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGetTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Sanitize: reject IDs containing path separators.
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	logPath := filepath.Join(d.gobrrDir, "logs", id+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"log not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to read log"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data) //nolint:errcheck
}

// --- memory handlers ---

type saveMemoryRequest struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Source  string   `json:"source"`
}

func (d *Daemon) handleSaveMemory(w http.ResponseWriter, r *http.Request) {
	var req saveMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, `{"error":"content is required"}`, http.StatusBadRequest)
		return
	}

	entry, err := d.memStore.Save(req.Content, req.Tags, req.Source)
	if err != nil {
		http.Error(w, `{"error":"failed to save memory"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry) //nolint:errcheck
}

func (d *Daemon) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tagsParam := r.URL.Query().Get("tags")
	limitParam := r.URL.Query().Get("limit")

	var tags []string
	if tagsParam != "" {
		for _, t := range strings.Split(tagsParam, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	limit := 0
	if limitParam != "" {
		if n, err := strconv.Atoi(limitParam); err == nil && n > 0 {
			limit = n
		}
	}

	var entries []*memory.Entry
	var err error
	if q != "" || len(tags) > 0 {
		entries, err = d.memStore.Search(q, tags, limit)
	} else {
		entries, err = d.memStore.List(limit)
	}
	if err != nil {
		http.Error(w, `{"error":"failed to search memory"}`, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []*memory.Entry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries) //nolint:errcheck
}

func (d *Daemon) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid memory id"}`, http.StatusBadRequest)
		return
	}

	entry, err := d.memStore.Get(id)
	if err != nil {
		http.Error(w, `{"error":"memory not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry) //nolint:errcheck
}

func (d *Daemon) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid memory id"}`, http.StatusBadRequest)
		return
	}

	if err := d.memStore.Delete(id); err != nil {
		http.Error(w, `{"error":"failed to delete memory"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Gmail handlers ---

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

// --- Calendar handlers ---

// gcalAccountRequest is the JSON body for calendar endpoints that only need account.
type gcalAccountRequest struct {
	Account string `json:"account"`
}

// gcalGetRequest is the JSON body for POST /gcal/get.
type gcalGetRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// gcalCreateRequest is the JSON body for POST /gcal/create.
type gcalCreateRequest struct {
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
	Account     string `json:"account"`
}

// gcalUpdateRequest is the JSON body for POST /gcal/update.
type gcalUpdateRequest struct {
	EventID string `json:"event_id"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Account string `json:"account"`
}

// gcalDeleteRequest is the JSON body for POST /gcal/delete.
type gcalDeleteRequest struct {
	EventID string `json:"event_id"`
	Account string `json:"account"`
}

// requireCalendar returns a CalendarAPI for the given account name. It writes
// an HTTP error response and returns nil if the account manager is not
// configured or the Calendar service cannot be created.
func (d *Daemon) requireCalendar(w http.ResponseWriter, account string) google.CalendarAPI {
	if d.accountMgr == nil {
		http.Error(w, `{"error":"Google accounts not configured"}`, http.StatusServiceUnavailable)
		return nil
	}
	httpClient, err := d.accountMgr.GetHTTPClient(account)
	if err != nil {
		http.Error(w, `{"error":"account not found or credentials unavailable"}`, http.StatusServiceUnavailable)
		return nil
	}
	svc, err := google.NewCalendarService(httpClient)
	if err != nil {
		http.Error(w, `{"error":"failed to create Calendar service"}`, http.StatusInternalServerError)
		return nil
	}
	return svc
}

func (d *Daemon) handleGcalToday(w http.ResponseWriter, r *http.Request) {
	var req gcalAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Account == "" {
		http.Error(w, `{"error":"account is required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	events, err := svc.Today()
	if err != nil {
		http.Error(w, `{"error":"failed to list today's events"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*google.EventSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events) //nolint:errcheck
}

func (d *Daemon) handleGcalWeek(w http.ResponseWriter, r *http.Request) {
	var req gcalAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Account == "" {
		http.Error(w, `{"error":"account is required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	events, err := svc.Week()
	if err != nil {
		http.Error(w, `{"error":"failed to list week's events"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*google.EventSummary{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events) //nolint:errcheck
}

func (d *Daemon) handleGcalGet(w http.ResponseWriter, r *http.Request) {
	var req gcalGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	detail, err := svc.GetEvent(req.EventID)
	if err != nil {
		http.Error(w, `{"error":"failed to get event"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail) //nolint:errcheck
}

func (d *Daemon) handleGcalCreate(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.Account == "" {
		http.Error(w, `{"error":"title and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.CreateEvent(req.Title, req.Start, req.End, req.Description); err != nil {
		http.Error(w, `{"error":"failed to create event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGcalUpdate(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.UpdateEvent(req.EventID, req.Title, req.Start, req.End); err != nil {
		http.Error(w, `{"error":"failed to update event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGcalDelete(w http.ResponseWriter, r *http.Request) {
	if !d.checkWritePermission(w, r) {
		return
	}

	var req gcalDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.EventID == "" || req.Account == "" {
		http.Error(w, `{"error":"event_id and account are required"}`, http.StatusBadRequest)
		return
	}

	svc := d.requireCalendar(w, req.Account)
	if svc == nil {
		return
	}

	if err := svc.DeleteEvent(req.EventID); err != nil {
		http.Error(w, `{"error":"failed to delete event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleApproveTask handles POST /tasks/{id}/approve.
// It signals the confirmation gate to approve the pending write action for the task.
//
// TODO: integrate gate.Request + gate.Wait into Gmail/Calendar write handlers so
// that the full approval flow (Telegram notification → wait → execute) is enforced.
func (d *Daemon) handleApproveTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.confirmGate.Approve(id); err != nil {
		http.Error(w, `{"error":"no pending confirmation for this task"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDenyTask handles POST /tasks/{id}/deny.
// It signals the confirmation gate to deny the pending write action for the task.
func (d *Daemon) handleDenyTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.confirmGate.Deny(id); err != nil {
		http.Error(w, `{"error":"no pending confirmation for this task"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadVaultIfAvailable attempts to load the master key and create a vault.
// Returns nil, nil if no key is available (not configured yet).
func loadVaultIfAvailable(gobrrDir string) (*vault.Vault, error) {
	key, err := vault.LoadMasterKey(gobrrDir)
	if err != nil {
		return nil, nil //nolint:nilerr // key absence is expected when not configured
	}
	return vault.New(key), nil
}

// --- Session handlers ---

func (d *Daemon) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if d.session == nil {
		json.NewEncoder(w).Encode(map[string]any{"enabled": false}) //nolint:errcheck
		return
	}
	pid, uptime, memMB, idle := d.session.Status()
	json.NewEncoder(w).Encode(map[string]any{
		"enabled": true,
		"running": d.session.Running(),
		"pid":     pid,
		"uptime":  uptime.String(),
		"mem_mb":  memMB,
		"idle":    idle.String(),
	}) //nolint:errcheck
}

func (d *Daemon) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	if d.session.Running() {
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	d.session.Start(d.ctx)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "starting"}) //nolint:errcheck
}

func (d *Daemon) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	d.session.Stop()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"}) //nolint:errcheck
}

func (d *Daemon) handleSessionRestart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	d.session.Stop()
	d.session.Start(d.ctx)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"}) //nolint:errcheck
}

// --- Scheduler handlers ---

func (d *Daemon) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Cron        string `json:"cron"`
		Prompt      string `json:"prompt"`
		ReplyTo     string `json:"reply_to"`
		AllowWrites bool   `json:"allow_writes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	sched, err := d.scheduler.Create(req.Name, req.Cron, req.Prompt, req.ReplyTo, req.AllowWrites)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sched) //nolint:errcheck
}

func (d *Daemon) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules := d.scheduler.List()
	if schedules == nil {
		schedules = []*scheduler.Schedule{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedules) //nolint:errcheck
}

func (d *Daemon) handleRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := d.scheduler.Remove(name); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "removed"}) //nolint:errcheck
}
