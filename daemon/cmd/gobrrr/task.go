package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/daemon"
)

// registerTask wires the root-level task verbs (submit/list/status/cancel/logs/approve/deny) onto root.
func registerTask(root *cobra.Command) {
	submitCmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new task",
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, _ := cmd.Flags().GetString("prompt")
			replyTo, _ := cmd.Flags().GetString("reply-to")
			priority, _ := cmd.Flags().GetInt("priority")
			allowWrites, _ := cmd.Flags().GetBool("allow-writes")
			timeout, _ := cmd.Flags().GetInt("timeout")
			warm, _ := cmd.Flags().GetBool("warm")

			if prompt == "" {
				return fmt.Errorf("--prompt is required")
			}
			c := newClient()
			task, err := c.SubmitTask(prompt, replyTo, priority, allowWrites, timeout, warm)
			if err != nil {
				return err
			}

			// When reply-to is stdout, block until the task completes and print
			// the result to stdout (or error to stderr with non-zero exit).
			if replyTo == "stdout" {
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
			all, _ := cmd.Flags().GetBool("all")
			c := newClient()
			tasks, err := c.ListTasks(all)
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

	submitCmd.Flags().String("prompt", "", "Task prompt (required)")
	submitCmd.Flags().String("reply-to", "channel", "Reply destination (e.g. channel, telegram, stdout)")
	submitCmd.Flags().Int("priority", 5, "Task priority (lower = higher priority)")
	submitCmd.Flags().Bool("allow-writes", false, "Allow file writes")
	submitCmd.Flags().Int("timeout", 300, "Timeout in seconds")
	submitCmd.Flags().Bool("warm", false, "Route to warm worker for fast dispatch")

	listCmd.Flags().Bool("all", false, "Include completed/failed tasks")

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
