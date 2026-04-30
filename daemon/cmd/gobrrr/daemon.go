package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
)

// registerDaemon wires the `daemon` verb (start/status) onto root.
func registerDaemon(root *cobra.Command) {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the gobrrr daemon",
	}

	daemonStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the gobrrr daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			gobrrDir := config.GobrrDir()
			if err := os.MkdirAll(gobrrDir, 0700); err != nil {
				return fmt.Errorf("creating gobrrr dir: %w", err)
			}

			cfg, err := config.Load(filepath.Join(gobrrDir, "config.json"))
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			socketPath := cfg.SocketPath
			if socketPath == "" {
				socketPath = filepath.Join(gobrrDir, "gobrrr.sock")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigs
				cancel()
			}()

			fmt.Fprintf(os.Stderr, "gobrrr daemon starting on %s\n", socketPath)
			d := daemon.New(cfg, socketPath)
			return d.Run(ctx)
		},
	}

	daemonStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			info, err := c.Health()
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			fmt.Printf("Status:          %v\n", info["status"])
			fmt.Printf("Uptime (sec):    %v\n", info["uptime_sec"])
			fmt.Printf("Workers active:  %v\n", info["workers_active"])
			fmt.Printf("Queue depth:     %v\n", info["queue_depth"])
			if ww, ok := info["warm_workers"].(map[string]any); ok {
				fmt.Printf("Warm workers:    total=%.0f, ready=%.0f, busy=%.0f, disabled=%.0f\n",
					ww["total"], ww["ready"], ww["busy"], ww["disabled"])
			}
			if models, ok := info["models"].(map[string]any); ok {
				fmt.Println("Models:")
				for _, role := range []string{"launcher", "warm_worker", "cold_worker"} {
					if m, ok := models[role].(map[string]any); ok {
						fmt.Printf("  %-12s %v (%v)\n", role, m["model"], m["permission_mode"])
					}
				}
			}
			return nil
		},
	}

	daemonCmd.AddCommand(daemonStartCmd, daemonStatusCmd)
	root.AddCommand(daemonCmd)
}
