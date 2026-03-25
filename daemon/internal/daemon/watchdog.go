package daemon

import (
	"context"
	"net"
	"os"
	"time"
)

// StartWatchdog sends systemd watchdog and ready notifications.
// Runs on a dedicated goroutine. No-op if not running under systemd.
func StartWatchdog(ctx context.Context) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}
	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		return
	}
	defer conn.Close()

	// Notify ready
	conn.Write([]byte("READY=1")) //nolint:errcheck

	// Watchdog loop — send every 30s (WatchdogSec=60 in unit file)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Write([]byte("STOPPING=1")) //nolint:errcheck
			return
		case <-ticker.C:
			conn.Write([]byte("WATCHDOG=1")) //nolint:errcheck
		}
	}
}
