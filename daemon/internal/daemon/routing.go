package daemon

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	vault "github.com/racterub/gobrrr/internal/crypto"
	"github.com/racterub/gobrrr/internal/security"
)

// routeResult delivers the result of a completed task to its designated
// reply_to destination(s). Supports comma-separated destinations for
// multi-destination routing. Before sending to telegram or channel, the
// result is scanned for potential credential leaks. If a leak is detected,
// the result is quarantined to the task log and a warning is sent instead.
func (d *Daemon) routeResult(task *Task, result string) error {
	destinations := strings.Split(task.ReplyTo, ",")
	var errs []error

	for _, dest := range destinations {
		dest = strings.TrimSpace(dest)
		if err := d.routeToDestination(task, result, dest); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dest, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("routing errors: %v", errors.Join(errs...))
	}
	return nil
}

func (d *Daemon) routeToDestination(task *Task, result, dest string) error {
	switch {
	case dest == "telegram":
		if d.notifier == nil {
			return fmt.Errorf("telegram not configured")
		}
		scan := security.Check(result, d.knownSecrets())
		if scan.HasLeak {
			d.quarantineResult(task, result, scan.Matches)
			return d.notifier.Send("\u26a0\ufe0f Task result contained potential credential leak and was quarantined. Check logs.")
		}
		return d.notifier.Send(result)
	case dest == "stdout":
		task.Result = &result
		return nil
	case strings.HasPrefix(dest, "file:"):
		path := strings.TrimPrefix(dest, "file:")
		return d.writeFileResult(path, result)
	case dest == "channel":
		scan := security.Check(result, d.knownSecrets())
		if scan.HasLeak {
			d.quarantineResult(task, result, scan.Matches)
			return fmt.Errorf("credential leak detected, result quarantined")
		}
		return d.emitToSSE(task, result)
	case dest == "":
		return nil
	default:
		return fmt.Errorf("unknown reply_to: %s", dest)
	}
}

// emitToSSE sends a task result to connected SSE clients.
func (d *Daemon) emitToSSE(task *Task, result string) error {
	// Placeholder — implemented in Task 6
	return nil
}

// knownSecrets returns a list of sensitive values that should never appear in
// task output. Currently includes the master key encoded as hex, which is the
// form it would appear if accidentally logged.
func (d *Daemon) knownSecrets() []string {
	key, err := vault.LoadMasterKey(d.gobrrDir)
	if err != nil {
		return nil
	}
	return []string{hex.EncodeToString(key[:])}
}

// quarantineResult appends the original result to the task's log file with a
// quarantine notice, and logs the leak matches.
func (d *Daemon) quarantineResult(task *Task, result string, matches []string) {
	logDir := filepath.Join(d.gobrrDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		log.Printf("quarantine: failed to create log dir: %v", err)
		return
	}
	logPath := filepath.Join(logDir, task.ID+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Printf("quarantine: failed to open log file %s: %v", logPath, err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "\n--- QUARANTINED RESULT [%s] ---\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(f, "Credential leak patterns detected: %s\n", strings.Join(matches, "; "))
	fmt.Fprintf(f, "--- BEGIN QUARANTINED OUTPUT ---\n%s\n--- END QUARANTINED OUTPUT ---\n", result)
	log.Printf("quarantine: task %s result quarantined due to potential credential leak (%d patterns)", task.ID, len(matches))
}

// writeFileResult writes result to the given path after validating it lies
// within an allowed directory.
func (d *Daemon) writeFileResult(rawPath, result string) error {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Try to resolve symlinks on the directory portion (the file itself may
	// not exist yet, so we only resolve up to the parent directory).
	resolved := absPath
	if dir := filepath.Dir(absPath); dirExists(dir) {
		if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
			resolved = filepath.Join(resolvedDir, filepath.Base(absPath))
		}
	}

	gobrrDir := config.GobrrDir()
	allowedPrefixes := []string{
		filepath.Join(gobrrDir, "output"),
		filepath.Join(os.TempDir(), "gobrrr"),
	}

	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(resolved, prefix+string(filepath.Separator)) || resolved == prefix {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("file path %s not in allowed directories (%s)", rawPath, strings.Join(allowedPrefixes, ", "))
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0700); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	return os.WriteFile(resolved, []byte(result), 0600)
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
