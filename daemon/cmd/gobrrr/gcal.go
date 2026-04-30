package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Gcal flag-value vars (Phase 2 will eliminate these).
var (
	gcalTodayAccount      string
	gcalWeekAccount       string
	gcalGetAccount        string
	gcalCreateTitle       string
	gcalCreateStart       string
	gcalCreateEnd         string
	gcalCreateDescription string
	gcalCreateAccount     string
	gcalUpdateTitle       string
	gcalUpdateStart       string
	gcalUpdateEnd         string
	gcalUpdateAccount     string
	gcalDeleteAccount     string
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
			c := newClient()
			result, err := c.GcalToday(gcalTodayAccount, os.Getenv("GOBRRR_TASK_ID"))
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
			c := newClient()
			result, err := c.GcalWeek(gcalWeekAccount, os.Getenv("GOBRRR_TASK_ID"))
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
			c := newClient()
			result, err := c.GcalGetEvent(args[0], gcalGetAccount, os.Getenv("GOBRRR_TASK_ID"))
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
			if gcalCreateTitle == "" {
				return fmt.Errorf("--title is required")
			}
			c := newClient()
			if err := c.GcalCreateEvent(gcalCreateTitle, gcalCreateStart, gcalCreateEnd, gcalCreateDescription, gcalCreateAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
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
			c := newClient()
			if err := c.GcalUpdateEvent(args[0], gcalUpdateTitle, gcalUpdateStart, gcalUpdateEnd, gcalUpdateAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
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
			c := newClient()
			if err := c.GcalDeleteEvent(args[0], gcalDeleteAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Printf("Event %s deleted.\n", args[0])
			return nil
		},
	}

	gcalTodayCmd.Flags().StringVar(&gcalTodayAccount, "account", "default", "Account name")
	gcalWeekCmd.Flags().StringVar(&gcalWeekAccount, "account", "default", "Account name")
	gcalGetCmd.Flags().StringVar(&gcalGetAccount, "account", "default", "Account name")

	gcalCreateCmd.Flags().StringVar(&gcalCreateTitle, "title", "", "Event title (required)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateStart, "start", "", "Event start time (RFC3339)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateEnd, "end", "", "Event end time (RFC3339)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateDescription, "description", "", "Event description")
	gcalCreateCmd.Flags().StringVar(&gcalCreateAccount, "account", "default", "Account name")

	gcalUpdateCmd.Flags().StringVar(&gcalUpdateTitle, "title", "", "New event title")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateStart, "start", "", "New event start time (RFC3339)")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateEnd, "end", "", "New event end time (RFC3339)")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateAccount, "account", "default", "Account name")

	gcalDeleteCmd.Flags().StringVar(&gcalDeleteAccount, "account", "default", "Account name")

	gcalCmd.AddCommand(gcalTodayCmd, gcalWeekCmd, gcalGetCmd, gcalCreateCmd, gcalUpdateCmd, gcalDeleteCmd)
	root.AddCommand(gcalCmd)
}
