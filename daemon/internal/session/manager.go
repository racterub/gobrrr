package session

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/racterub/gobrrr/internal/config"
)

// readCgroupMemoryMB reads the cgroup memory.current in MB.
// It reads the service-specific cgroup via /proc/self/cgroup first,
// falling back to the root cgroup. Returns 0 if unavailable.
func readCgroupMemoryMB() int {
	// Try service-specific cgroup first via /proc/self/cgroup.
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		// cgroup v2: single line "0::/system.slice/gobrrr.service"
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "0::") {
				rel := strings.TrimSpace(strings.TrimPrefix(line, "0::"))
				if rel != "" && rel != "/" {
					path := "/sys/fs/cgroup" + rel + "/memory.current"
					if b, err := os.ReadFile(path); err == nil {
						if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
							return int(v / 1024 / 1024)
						}
					}
				}
			}
		}
	}
	// Fallback to root cgroup.
	data, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return int(bytes / 1024 / 1024)
}

// Notifier sends alert messages (e.g., to Telegram).
type Notifier interface {
	Send(msg string) error
}

// Manager supervises a Claude Code channel-mode process.
type Manager struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	ptmx       *os.File
	startedAt  time.Time
	lastOutput time.Time
	restarts   int
	running    bool
	config     config.TelegramSessionConfig
	notifier   Notifier
	workDir    string
}

// NewManager creates a session manager. Notifier may be nil.
func NewManager(cfg config.TelegramSessionConfig, notifier Notifier) *Manager {
	return &Manager{
		config:   cfg,
		notifier: notifier,
	}
}

// SetWorkDir sets the working directory for the Claude session.
func (m *Manager) SetWorkDir(dir string) {
	m.workDir = dir
}

// Running returns whether the session process is alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Status returns session status info.
func (m *Manager) Status() (pid int, uptime time.Duration, memMB int, idleFor time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		return 0, 0, 0, 0
	}

	pid = m.cmd.Process.Pid
	uptime = time.Since(m.startedAt)
	memMB = readCgroupMemoryMB()
	idleFor = time.Since(m.lastOutput)
	return
}

// BackoffFor returns the backoff duration for the given restart attempt.
func (m *Manager) BackoffFor(attempt int) time.Duration {
	backoffs := []time.Duration{
		30 * time.Second,
		60 * time.Second,
		120 * time.Second,
		300 * time.Second,
	}
	if attempt >= len(backoffs) {
		return backoffs[len(backoffs)-1]
	}
	return backoffs[attempt]
}

// EvalRotation decides whether the session should be rotated.
func (m *Manager) EvalRotation(memMB int, uptime time.Duration, idleDuration time.Duration) (string, bool) {
	cfg := m.config

	if cfg.MemoryCeilingMB > 0 && memMB > cfg.MemoryCeilingMB {
		return fmt.Sprintf("memory %dMB > ceiling %dMB", memMB, cfg.MemoryCeilingMB), true
	}

	maxUptime := time.Duration(cfg.MaxUptimeHours) * time.Hour
	idleThreshold := time.Duration(cfg.IdleThresholdMin) * time.Minute

	if cfg.MaxUptimeHours > 0 && uptime > maxUptime && idleDuration > idleThreshold {
		return fmt.Sprintf("uptime %s > max %s (idle %s)", uptime.Truncate(time.Minute), maxUptime, idleDuration.Truncate(time.Minute)), true
	}

	return "", false
}

// Run starts the session supervisor loop. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	if !m.config.Enabled {
		log.Println("session: telegram session disabled")
		return
	}

	for {
		if m.restarts >= m.config.MaxRestartAttempts {
			msg := fmt.Sprintf("session: gave up after %d consecutive failures", m.restarts)
			log.Println(msg)
			m.alert(msg)
			return
		}

		err := m.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}

		sessionDuration := time.Since(m.startedAt)
		if sessionDuration > 5*time.Minute {
			m.restarts = 0
		} else {
			m.restarts++
		}

		if err != nil {
			log.Printf("session: Claude exited with error: %v", err)
		}

		backoff := m.BackoffFor(m.restarts)
		if m.restarts >= 3 {
			m.alert(fmt.Sprintf("session: restart attempt %d, backing off %s", m.restarts+1, backoff))
		}

		log.Printf("session: backing off %s before restart (attempt %d)", backoff, m.restarts)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runOnce spawns a single Claude session and monitors it until it exits or is rotated.
func (m *Manager) runOnce(ctx context.Context) error {
	args := []string{"--dangerously-skip-permissions"}
	for _, ch := range m.config.Channels {
		args = append(args, "--channels", ch)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	if m.workDir != "" {
		cmd.Dir = m.workDir
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start claude with pty: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	m.ptmx = ptmx
	m.startedAt = time.Now()
	m.lastOutput = time.Now()
	m.running = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.cmd = nil
		m.ptmx = nil
		m.mu.Unlock()
		ptmx.Close()
	}()

	log.Printf("session: Claude started (PID %d)", cmd.Process.Pid)

	var outputWg sync.WaitGroup
	outputWg.Add(1)
	go func() {
		defer outputWg.Done()
		m.readOutput(ptmx)
	}()

	exited := make(chan struct{})

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		m.monitorLoop(ctx, cmd, exited)
	}()

	waitErr := cmd.Wait()
	close(exited)

	<-monitorDone
	outputWg.Wait()

	log.Printf("session: Claude exited (PID %d)", cmd.Process.Pid)
	return waitErr
}

// readOutput reads from the PTY and updates lastOutput timestamp.
func (m *Manager) readOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.mu.Lock()
			m.lastOutput = time.Now()
			m.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// monitorLoop checks rotation conditions every 60 seconds.
func (m *Manager) monitorLoop(ctx context.Context, cmd *exec.Cmd, exited <-chan struct{}) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-exited:
			return
		case <-ticker.C:
			m.mu.Lock()
			running := m.running
			startedAt := m.startedAt
			lastOutput := m.lastOutput
			m.mu.Unlock()

			if !running {
				return
			}

			memMB := readCgroupMemoryMB()
			uptime := time.Since(startedAt)
			idleDuration := time.Since(lastOutput)

			reason, shouldRotate := m.EvalRotation(memMB, uptime, idleDuration)
			if shouldRotate {
				log.Printf("session: rotating — %s", reason)
				m.alert(fmt.Sprintf("session: rotating — %s", reason))
				m.killProcess(cmd)
				return
			}
		}
	}
}

// killProcess sends SIGTERM, then SIGKILL after 60s if still alive.
// It does NOT call cmd.Wait() — the caller (runOnce) is responsible for reaping.
func (m *Manager) killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		time.Sleep(60 * time.Second)
		// If process is still alive, escalate to SIGKILL.
		if cmd.ProcessState == nil {
			_ = cmd.Process.Signal(syscall.SIGKILL)
		}
	}()
}

// Stop gracefully stops the current session and waits for it to exit.
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	m.killProcess(cmd)

	// Wait for runOnce to finish reaping the process.
	deadline := time.After(90 * time.Second)
	for {
		select {
		case <-deadline:
			return
		case <-time.After(200 * time.Millisecond):
			m.mu.Lock()
			still := m.running
			m.mu.Unlock()
			if !still {
				return
			}
		}
	}
}

func (m *Manager) alert(msg string) {
	if m.notifier != nil {
		_ = m.notifier.Send(msg)
	}
}

// Start launches the session supervisor in a new goroutine.
// Can be called after Stop() or after max_restart_attempts to restart the loop.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.restarts = 0
	m.mu.Unlock()
	go m.Run(ctx)
}
