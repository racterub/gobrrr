package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// registerSession wires the `session` verb (status/start/stop/restart) onto root.
func registerSession(root *cobra.Command) {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage the Telegram channel session",
	}

	sessionStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show session status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			status, err := c.SessionStatus()
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(status, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	sessionStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionStart(); err != nil {
				return err
			}
			fmt.Println("Session starting")
			return nil
		},
	}

	sessionStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionStop(); err != nil {
				return err
			}
			fmt.Println("Session stopped")
			return nil
		},
	}

	sessionRestartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionRestart(); err != nil {
				return err
			}
			fmt.Println("Session restarting")
			return nil
		},
	}

	sessionCmd.AddCommand(sessionStatusCmd, sessionStartCmd, sessionStopCmd, sessionRestartCmd)
	root.AddCommand(sessionCmd)
}
