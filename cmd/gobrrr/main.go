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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "gobrrr",
	Short: "gobrrr — a Go daemon that dispatches Claude Code tasks",
	Long: `gobrrr is a daemon that dispatches Claude Code tasks with built-in
Gmail and Calendar integration. It listens on a Unix socket and
spawns claude workers to execute approved tasks.`,
}

// daemonCmd groups daemon lifecycle subcommands.
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the gobrrr daemon",
}

var daemonStartCmd = &cobra.Command{
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

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// submitCmd submits a new task.
var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a new task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// listCmd lists tasks.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// statusCmd shows task status.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show task status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// cancelCmd cancels a task.
var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// logsCmd shows task logs.
var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show task logs",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// approveCmd approves a pending task.
var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve a pending task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// denyCmd denies a pending task.
var denyCmd = &cobra.Command{
	Use:   "deny",
	Short: "Deny a pending task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// gmailCmd provides Gmail integration commands.
var gmailCmd = &cobra.Command{
	Use:   "gmail",
	Short: "Gmail integration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// gcalCmd provides Google Calendar integration commands.
var gcalCmd = &cobra.Command{
	Use:   "gcal",
	Short: "Google Calendar integration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// memoryCmd manages daemon memory.
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage daemon memory",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// setupCmd runs first-time setup.
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run first-time setup",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)

	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(submitCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(denyCmd)
	rootCmd.AddCommand(gmailCmd)
	rootCmd.AddCommand(gcalCmd)
	rootCmd.AddCommand(memoryCmd)
	rootCmd.AddCommand(setupCmd)
}
