package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/daemon"
)

// Submit flag-value vars (Phase 2 will eliminate these in favor of cmd.Flags().Get*).
var (
	submitPrompt      string
	submitReplyTo     string
	submitPriority    int
	submitAllowWrites bool
	submitTimeout     int
	submitWarm        bool
)

// List flag-value var (Phase 2 will eliminate this).
var listAll bool

// registerTask wires the root-level task verbs (submit/list/status/cancel/logs/approve/deny) onto root.
func registerTask(root *cobra.Command) {
	submitCmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if submitPrompt == "" {
				return fmt.Errorf("--prompt is required")
			}
			c := newClient()
			task, err := c.SubmitTask(submitPrompt, submitReplyTo, submitPriority, submitAllowWrites, submitTimeout, submitWarm)
			if err != nil {
				return err
			}

			// When reply-to is stdout, block until the task completes and print
			// the result to stdout (or error to stderr with non-zero exit).
			if submitReplyTo == "stdout" {
				result, waitErr := c.WaitForTask(task.ID)
				if waitErr != nil {
					// Connection loss: exit 2; task failure: exit 1.
					if strings.Contains(waitErr.Error(), "daemon connection lost") {
						fmt.Fprintln(os.Stderr, waitErr.Error())
						os.Exit(2)
					}
					fmt.Fprintln(os.Stderr, waitErr.Error())
					os.Exit(1)
				}
				fmt.Print(result)
				return nil
			}

			printTask(task)
			return nil
		},
	}

	listCmd := &cobra.Command{
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

	statusCmd := &cobra.Command{
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

	cancelCmd := &cobra.Command{
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

	logsCmd := &cobra.Command{
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

	// Top-level approve/deny act on any pending approval id (skill_install today,
	// write_action soon). Per-kind subcommands like `gobrrr skill approve` still
	// exist when a kind needs extra options (e.g. --skip-binary).
	approveCmd := &cobra.Command{
		Use:   "approve <approval-id>",
		Short: "Approve a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "approve"); err != nil {
				return err
			}
			fmt.Println("approved")
			return nil
		},
	}

	denyCmd := &cobra.Command{
		Use:   "deny <approval-id>",
		Short: "Deny a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "deny"); err != nil {
				return err
			}
			fmt.Println("denied")
			return nil
		},
	}

	submitCmd.Flags().StringVar(&submitPrompt, "prompt", "", "Task prompt (required)")
	submitCmd.Flags().StringVar(&submitReplyTo, "reply-to", "channel", "Reply destination (e.g. channel, telegram, stdout)")
	submitCmd.Flags().IntVar(&submitPriority, "priority", 5, "Task priority (lower = higher priority)")
	submitCmd.Flags().BoolVar(&submitAllowWrites, "allow-writes", false, "Allow file writes")
	submitCmd.Flags().IntVar(&submitTimeout, "timeout", 300, "Timeout in seconds")
	submitCmd.Flags().BoolVar(&submitWarm, "warm", false, "Route to warm worker for fast dispatch")

	listCmd.Flags().BoolVar(&listAll, "all", false, "Include completed/failed tasks")

	root.AddCommand(submitCmd, listCmd, statusCmd, cancelCmd, logsCmd, approveCmd, denyCmd)
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
