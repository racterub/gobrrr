// Package daemon implements the gobrrr HTTP daemon that listens on a Unix socket.
package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/racterub/gobrrr/internal/clawhub"
	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/google"
	"github.com/racterub/gobrrr/internal/memory"
	"github.com/racterub/gobrrr/internal/scheduler"
	"github.com/racterub/gobrrr/internal/session"
	"github.com/racterub/gobrrr/internal/skills"
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
	startTime     time.Time
	session       *session.Manager
	scheduler     *scheduler.Scheduler
	skillReg      *skills.Registry
	skillsRoot    string
	clawhub       *clawhub.Client
	installer     *clawhub.Installer
	committer     *clawhub.Committer
	approvals     *ApprovalDispatcher
	approvalsRoot string
	approvalHub   *ApprovalHub
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

	skillsRoot := filepath.Join(gobrrDir, "skills")
	if err := skills.InstallSystemSkills(skillsRoot); err != nil {
		log.Printf("daemon: install system skills: %v", err)
	}
	skillReg := skills.NewRegistry(skillsRoot)
	if err := skillReg.Refresh(); err != nil {
		log.Printf("daemon: refresh skills: %v", err)
	}

	// ClawHub wiring. Empty URL falls back to clawhub.DefaultBaseURL.
	ch := clawhub.NewClient(cfg.ClawHub.RegistryURL)
	installer := clawhub.NewInstaller(skillsRoot, cfg.ClawHub.RegistryURL, binOnPath)
	committer := clawhub.NewCommitter(skillsRoot, nil)

	approvalsRoot := gobrrDir
	approvalStore := NewApprovalStore(approvalsRoot)
	approvals := NewApprovalDispatcher(approvalStore)
	approvalHub := NewApprovalHub()
	approvals.SetCallbacks(
		func(r *ApprovalRequest) {
			approvalHub.Emit(ApprovalEvent{Type: ApprovalEventCreated, Request: r})
		},
		func(id, dec, errMsg string) {
			approvalHub.Emit(ApprovalEvent{Type: ApprovalEventRemoved, ID: id, Decision: dec, Error: errMsg})
		},
	)
	approvals.Register("skill_install", &skillInstallHandler{committer: committer})

	wp := NewWorkerPool(q, cfg, cfg.MaxWorkers, spawnInterval, gobrrDir, ms, skillReg)

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
		skillReg:      skillReg,
		skillsRoot:    skillsRoot,
		clawhub:       ch,
		installer:     installer,
		committer:     committer,
		approvals:     approvals,
		approvalsRoot: approvalsRoot,
		approvalHub:   approvalHub,
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

	d.registerSkillRoutes()
	d.mux.HandleFunc("POST /approvals/{id}", approvalDecisionHandler(d.approvals))
	d.mux.HandleFunc("GET /approvals/stream", approvalStreamHandler(d.approvals, d.approvalHub))

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
