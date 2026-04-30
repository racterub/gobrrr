package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/config"
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

func init() {
	registerDaemon(rootCmd)
	registerTask(rootCmd)
	registerGmail(rootCmd)
	registerGcal(rootCmd)
	registerMemory(rootCmd)
	registerSession(rootCmd)
	registerTimer(rootCmd)
	registerSkill(rootCmd)
	registerSetup(rootCmd)
}

// newClient creates a Client connected to the configured socket path.
func newClient() *client.Client {
	gobrrDir := config.GobrrDir()
	socketPath := filepath.Join(gobrrDir, "gobrrr.sock")
	return client.New(socketPath)
}
