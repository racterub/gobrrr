package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/racterub/gobrrr/internal/config"
)

// routeResult delivers the result of a completed task to its designated
// reply_to destination.
func (d *Daemon) routeResult(task *Task, result string) error {
	switch {
	case task.ReplyTo == "telegram":
		if d.notifier == nil {
			return fmt.Errorf("telegram not configured")
		}
		return d.notifier.Send(result)
	case task.ReplyTo == "stdout":
		// Result is already stored on the task via queue.Complete; polling
		// clients will pick it up via GET /tasks/{id}.
		return nil
	case strings.HasPrefix(task.ReplyTo, "file:"):
		return d.writeFileResult(task.ReplyTo[5:], result)
	default:
		if task.ReplyTo == "" {
			return nil
		}
		return fmt.Errorf("unknown reply_to: %s", task.ReplyTo)
	}
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
