package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/skills"
)

// registerSkill wires the `skill` verb (list/search/install/approve/deny/uninstall) onto root.
func registerSkill(root *cobra.Command) {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage worker skills",
	}

	skillListCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli := newClient()
			list, err := cli.ListSkills()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("No skills installed.")
				return nil
			}
			byType := map[string][]skills.Skill{}
			for _, s := range list {
				byType[string(s.Type)] = append(byType[string(s.Type)], s)
			}
			for _, t := range []string{"system", "clawhub", "user"} {
				if len(byType[t]) == 0 {
					continue
				}
				fmt.Printf("[%s]\n", t)
				for _, s := range byType[t] {
					fmt.Printf("  %-20s  %s\n", s.Slug, s.Description)
				}
			}
			return nil
		},
	}

	skillSearchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search ClawHub for skills",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := newClient().SearchSkills(args[0])
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("No matches.")
				return nil
			}
			for _, r := range results {
				summary := ""
				if r.Summary != nil {
					summary = *r.Summary
				}
				fmt.Printf("%-24s  %s\n", r.Slug, summary)
			}
			return nil
		},
	}

	skillInstallCmd := &cobra.Command{
		Use:   "install <slug>[@version]",
		Short: "Stage a ClawHub skill install (prints approval card)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, version := parseSlugVersion(args[0])
			res, err := newClient().InstallSkill(slug, version)
			if err != nil {
				return err
			}
			printApprovalCard(res)
			return nil
		},
	}

	skillApproveCmd := &cobra.Command{
		Use:   "approve <request-id>",
		Short: "Approve a staged skill install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skip, _ := cmd.Flags().GetBool("skip-binary")
			decision := "approve"
			if skip {
				decision = "skip_binary"
			}
			if err := newClient().DecideApproval(args[0], decision); err != nil {
				return err
			}
			fmt.Println("approved")
			return nil
		},
	}

	skillDenyCmd := &cobra.Command{
		Use:   "deny <request-id>",
		Short: "Deny a staged skill install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "deny"); err != nil {
				return err
			}
			fmt.Println("denied")
			return nil
		},
	}

	skillUninstallCmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().UninstallSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("uninstalled %s\n", args[0])
			return nil
		},
	}

	skillApproveCmd.Flags().Bool("skip-binary", false, "approve skill only, skip binary install commands")
	skillCmd.AddCommand(skillListCmd, skillSearchCmd, skillInstallCmd, skillApproveCmd, skillDenyCmd, skillUninstallCmd)
	root.AddCommand(skillCmd)
}

func parseSlugVersion(s string) (string, string) {
	if i := strings.Index(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func printApprovalCard(r *client.InstallResult) {
	req := r.Request
	fmt.Printf("Install skill: %s@%s\n", req.Slug, req.Version)
	fmt.Printf("  Source: %s\n  sha256: %s\n", req.SourceURL, req.SHA256)
	if req.Frontmatter.Description != "" {
		fmt.Printf("  Description: %s\n", req.Frontmatter.Description)
	}
	fmt.Println()

	if len(req.MissingBins) > 0 {
		fmt.Printf("  Requires binaries: %s  (not on PATH)\n", strings.Join(req.MissingBins, ", "))
		for _, p := range req.ProposedCommands {
			fmt.Printf("    Proposed install:  %s\n", p.Command)
		}
		fmt.Println()
	}

	reads := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Read
	writes := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Write
	if len(reads) > 0 {
		fmt.Println("  Tool permissions (read, always allowed):")
		for _, p := range reads {
			fmt.Printf("    %s\n", p)
		}
	}
	if len(writes) > 0 {
		fmt.Println("\n  Tool permissions (write, require --allow-writes on task):")
		for _, p := range writes {
			fmt.Printf("    %s\n", p)
		}
	}

	fmt.Printf("\n  Request ID: %s\n\n", r.RequestID)
	fmt.Printf("  To proceed:  gobrrr skill approve %s\n", r.RequestID)
	fmt.Printf("  Skill only:  gobrrr skill approve %s --skip-binary\n", r.RequestID)
	fmt.Printf("  Cancel:      gobrrr skill deny %s\n", r.RequestID)
	fmt.Printf("  (Inline approval also available via Telegram once the bot is running.)\n")
}
