package main

import (
	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/setup"
)

// registerSetup wires the `setup` verb (and its `google-account` subcommand) onto root.
func registerSetup(root *cobra.Command) {
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Run first-time setup wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.RunWizard()
		},
	}

	setupGoogleAccountCmd := &cobra.Command{
		Use:   "google-account",
		Short: "Add a Google account",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			return setup.RunGoogleAccountSetup(name)
		},
	}

	setupGoogleAccountCmd.Flags().String("name", "", "Account label (e.g. personal, work)")
	setupCmd.AddCommand(setupGoogleAccountCmd)
	root.AddCommand(setupCmd)
}
