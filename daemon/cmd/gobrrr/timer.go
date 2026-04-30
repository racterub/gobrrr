package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// registerTimer wires the `timer` verb (create/list/remove) onto root.
func registerTimer(root *cobra.Command) {
	timerCmd := &cobra.Command{
		Use:   "timer",
		Short: "Manage scheduled tasks",
	}

	timerCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a recurring scheduled task",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			cronExpr, _ := cmd.Flags().GetString("cron")
			prompt, _ := cmd.Flags().GetString("prompt")
			replyTo, _ := cmd.Flags().GetString("reply-to")
			allowWrites, _ := cmd.Flags().GetBool("allow-writes")

			c := newClient()
			result, err := c.CreateSchedule(name, cronExpr, prompt, replyTo, allowWrites)
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	timerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all scheduled tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			schedules, err := c.ListSchedules()
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(schedules, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	timerRemoveCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a scheduled task",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			c := newClient()
			if err := c.RemoveSchedule(name); err != nil {
				return err
			}
			fmt.Printf("Removed schedule %q\n", name)
			return nil
		},
	}

	timerCreateCmd.Flags().String("name", "", "Schedule name (required)")
	timerCreateCmd.Flags().String("cron", "", "Cron expression (required)")
	timerCreateCmd.Flags().String("prompt", "", "Task prompt (required)")
	timerCreateCmd.Flags().String("reply-to", "channel", "Result destination")
	timerCreateCmd.Flags().Bool("allow-writes", false, "Allow write operations")
	timerCreateCmd.MarkFlagRequired("name")
	timerCreateCmd.MarkFlagRequired("cron")
	timerCreateCmd.MarkFlagRequired("prompt")

	timerRemoveCmd.Flags().String("name", "", "Schedule name (required)")
	timerRemoveCmd.MarkFlagRequired("name")

	timerCmd.AddCommand(timerCreateCmd, timerListCmd, timerRemoveCmd)
	root.AddCommand(timerCmd)
}
