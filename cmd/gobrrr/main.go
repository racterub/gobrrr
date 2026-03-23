package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/racterub/gobrrr/internal/memory"
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
		return nil
	},
}

// --- submit ---

var (
	submitPrompt      string
	submitReplyTo     string
	submitPriority    int
	submitAllowWrites bool
	submitTimeout     int
)

var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a new task",
	RunE: func(cmd *cobra.Command, args []string) error {
		if submitPrompt == "" {
			return fmt.Errorf("--prompt is required")
		}
		c := newClient()
		task, err := c.SubmitTask(submitPrompt, submitReplyTo, submitPriority, submitAllowWrites, submitTimeout)
		if err != nil {
			return err
		}
		printTask(task)
		return nil
	},
}

// --- list ---

var listAll bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		tasks, err := c.ListTasks(listAll)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Println("No tasks.")
			return nil
		}
		for _, t := range tasks {
			printTaskSummary(t)
		}
		return nil
	},
}

// --- status (task) ---

var statusCmd = &cobra.Command{
	Use:   "status <id>",
	Short: "Show task status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		task, err := c.GetTask(args[0])
		if err != nil {
			return err
		}
		printTask(task)
		return nil
	},
}

// --- cancel ---

var cancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		if err := c.CancelTask(args[0]); err != nil {
			return err
		}
		fmt.Printf("Task %s cancelled.\n", args[0])
		return nil
	},
}

// --- logs ---

var logsCmd = &cobra.Command{
	Use:   "logs <id>",
	Short: "Show task logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		logs, err := c.GetLogs(args[0])
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil
	},
}

// --- approve / deny / gmail / gcal / memory / setup (stubs) ---

var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve a pending task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

var denyCmd = &cobra.Command{
	Use:   "deny",
	Short: "Deny a pending task",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

var gmailCmd = &cobra.Command{
	Use:   "gmail",
	Short: "Gmail integration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

var gcalCmd = &cobra.Command{
	Use:   "gcal",
	Short: "Google Calendar integration",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
	},
}

// --- memory ---

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage daemon memory",
}

var (
	memorySaveContent string
	memorySaveTags    string
	memorySaveSource  string
)

var memorySaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save a memory entry",
	RunE: func(cmd *cobra.Command, args []string) error {
		if memorySaveContent == "" {
			return fmt.Errorf("--content is required")
		}
		var tags []string
		if memorySaveTags != "" {
			for _, t := range strings.Split(memorySaveTags, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
		c := newClient()
		entry, err := c.SaveMemory(memorySaveContent, tags, memorySaveSource)
		if err != nil {
			return err
		}
		printMemoryEntry(entry)
		return nil
	},
}

var (
	memorySearchTags string
)

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memory entries",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var tags []string
		if memorySearchTags != "" {
			for _, t := range strings.Split(memorySearchTags, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
		c := newClient()
		entries, err := c.SearchMemory(args[0], tags, 0)
		if err != nil {
			return err
		}
		printMemoryList(entries)
		return nil
	},
}

var memoryListLimit int

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memory entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		entries, err := c.SearchMemory("", nil, memoryListLimit)
		if err != nil {
			return err
		}
		printMemoryList(entries)
		return nil
	},
}

var memoryGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a memory entry by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		entry, err := c.GetMemory(args[0])
		if err != nil {
			return err
		}
		printMemoryEntry(entry)
		return nil
	},
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a memory entry by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		if err := c.DeleteMemory(args[0]); err != nil {
			return err
		}
		fmt.Printf("Memory %s deleted.\n", args[0])
		return nil
	},
}

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

	submitCmd.Flags().StringVar(&submitPrompt, "prompt", "", "Task prompt (required)")
	submitCmd.Flags().StringVar(&submitReplyTo, "reply-to", "", "Reply destination (e.g. telegram)")
	submitCmd.Flags().IntVar(&submitPriority, "priority", 5, "Task priority (lower = higher priority)")
	submitCmd.Flags().BoolVar(&submitAllowWrites, "allow-writes", false, "Allow file writes")
	submitCmd.Flags().IntVar(&submitTimeout, "timeout", 300, "Timeout in seconds")

	listCmd.Flags().BoolVar(&listAll, "all", false, "Include completed/failed tasks")

	memorySaveCmd.Flags().StringVar(&memorySaveContent, "content", "", "Memory content (required)")
	memorySaveCmd.Flags().StringVar(&memorySaveTags, "tags", "", "Comma-separated tags")
	memorySaveCmd.Flags().StringVar(&memorySaveSource, "source", "", "Source of the memory")

	memorySearchCmd.Flags().StringVar(&memorySearchTags, "tags", "", "Comma-separated tags to filter by")

	memoryListCmd.Flags().IntVar(&memoryListLimit, "limit", 20, "Maximum number of entries to return")

	memoryCmd.AddCommand(memorySaveCmd)
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryGetCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)

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

// newClient creates a Client connected to the configured socket path.
func newClient() *client.Client {
	gobrrDir := config.GobrrDir()
	socketPath := filepath.Join(gobrrDir, "gobrrr.sock")
	return client.New(socketPath)
}

// printTask prints full task details.
func printTask(t *daemon.Task) {
	fmt.Printf("ID:          %s\n", t.ID)
	fmt.Printf("Status:      %s\n", t.Status)
	fmt.Printf("Priority:    %d\n", t.Priority)
	fmt.Printf("Prompt:      %s\n", t.Prompt)
	fmt.Printf("Reply-To:    %s\n", t.ReplyTo)
	fmt.Printf("Allow Writes:%v\n", t.AllowWrites)
	fmt.Printf("Created:     %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
	if t.StartedAt != nil {
		fmt.Printf("Started:     %s\n", t.StartedAt.Format("2006-01-02 15:04:05"))
	}
	if t.CompletedAt != nil {
		fmt.Printf("Completed:   %s\n", t.CompletedAt.Format("2006-01-02 15:04:05"))
	}
	if t.Result != nil {
		fmt.Printf("Result:      %s\n", *t.Result)
	}
	if t.Error != nil {
		fmt.Printf("Error:       %s\n", *t.Error)
	}
}

// printTaskSummary prints a one-line summary.
func printTaskSummary(t *daemon.Task) {
	fmt.Printf("%-26s  %-10s  p%-3d  %s\n", t.ID, t.Status, t.Priority, t.Prompt)
}

// printMemoryEntry prints full details of a memory entry.
func printMemoryEntry(e *memory.Entry) {
	fmt.Printf("ID:         %s\n", e.ID)
	fmt.Printf("Source:     %s\n", e.Source)
	fmt.Printf("Tags:       %s\n", strings.Join(e.Tags, ", "))
	fmt.Printf("Created:    %s\n", e.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Content:    %s\n", e.Content)
}

// printMemoryList prints a compact list of memory entries.
func printMemoryList(entries []*memory.Entry) {
	if len(entries) == 0 {
		fmt.Println("No memory entries.")
		return
	}
	for _, e := range entries {
		summary := e.Content
		if len(summary) > 60 {
			summary = summary[:60] + "..."
		}
		fmt.Printf("%-30s  %-20s  %s\n", e.ID, strings.Join(e.Tags, ","), summary)
	}
}
