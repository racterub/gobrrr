package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// registerGcal wires the `gcal` verb (today/week/get/create/update/delete) onto root.
func registerGcal(root *cobra.Command) {
	gcalCmd := &cobra.Command{
		Use:   "gcal",
		Short: "Google Calendar integration",
	}

	gcalTodayCmd := &cobra.Command{
		Use:   "today",
		Short: "List today's calendar events",
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			result, err := c.GcalToday(account, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalWeekCmd := &cobra.Command{
		Use:   "week",
		Short: "List this week's calendar events",
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			result, err := c.GcalWeek(account, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalGetCmd := &cobra.Command{
		Use:   "get <event-id>",
		Short: "Get a calendar event by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			result, err := c.GcalGetEvent(args[0], account, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a calendar event",
		RunE: func(cmd *cobra.Command, args []string) error {
			title, _ := cmd.Flags().GetString("title")
			start, _ := cmd.Flags().GetString("start")
			end, _ := cmd.Flags().GetString("end")
			description, _ := cmd.Flags().GetString("description")
			account, _ := cmd.Flags().GetString("account")
			if title == "" {
				return fmt.Errorf("--title is required")
			}
			c := newClient()
			if err := c.GcalCreateEvent(title, start, end, description, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Event created.")
			return nil
		},
	}

	gcalUpdateCmd := &cobra.Command{
		Use:   "update <event-id>",
		Short: "Update a calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title, _ := cmd.Flags().GetString("title")
			start, _ := cmd.Flags().GetString("start")
			end, _ := cmd.Flags().GetString("end")
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			if err := c.GcalUpdateEvent(args[0], title, start, end, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Event updated.")
			return nil
		},
	}

	gcalDeleteCmd := &cobra.Command{
		Use:   "delete <event-id>",
		Short: "Delete a calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			if err := c.GcalDeleteEvent(args[0], account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Printf("Event %s deleted.\n", args[0])
			return nil
		},
	}

	gcalTodayCmd.Flags().String("account", "default", "Account name")
	gcalWeekCmd.Flags().String("account", "default", "Account name")
	gcalGetCmd.Flags().String("account", "default", "Account name")

	gcalCreateCmd.Flags().String("title", "", "Event title (required)")
	gcalCreateCmd.Flags().String("start", "", "Event start time (RFC3339)")
	gcalCreateCmd.Flags().String("end", "", "Event end time (RFC3339)")
	gcalCreateCmd.Flags().String("description", "", "Event description")
	gcalCreateCmd.Flags().String("account", "default", "Account name")

	gcalUpdateCmd.Flags().String("title", "", "New event title")
	gcalUpdateCmd.Flags().String("start", "", "New event start time (RFC3339)")
	gcalUpdateCmd.Flags().String("end", "", "New event end time (RFC3339)")
	gcalUpdateCmd.Flags().String("account", "default", "Account name")

	gcalDeleteCmd.Flags().String("account", "default", "Account name")

	gcalCmd.AddCommand(gcalTodayCmd, gcalWeekCmd, gcalGetCmd, gcalCreateCmd, gcalUpdateCmd, gcalDeleteCmd)
	root.AddCommand(gcalCmd)
}
