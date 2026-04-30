package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// registerGmail wires the `gmail` verb (list/read/send/reply) onto root.
func registerGmail(root *cobra.Command) {
	gmailCmd := &cobra.Command{
		Use:   "gmail",
		Short: "Gmail integration",
	}

	gmailListCmd := &cobra.Command{
		Use:   "list",
		Short: "List Gmail messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			unread, _ := cmd.Flags().GetBool("unread")
			query, _ := cmd.Flags().GetString("query")
			limit, _ := cmd.Flags().GetInt("limit")
			account, _ := cmd.Flags().GetString("account")
			if unread && query == "" {
				query = "is:unread"
			} else if unread {
				query = "is:unread " + query
			}
			c := newClient()
			result, err := c.GmailList(query, limit, account, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gmailReadCmd := &cobra.Command{
		Use:   "read <message-id>",
		Short: "Read a Gmail message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			c := newClient()
			result, err := c.GmailRead(args[0], account, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gmailSendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a Gmail message",
		RunE: func(cmd *cobra.Command, args []string) error {
			to, _ := cmd.Flags().GetString("to")
			subject, _ := cmd.Flags().GetString("subject")
			body, _ := cmd.Flags().GetString("body")
			account, _ := cmd.Flags().GetString("account")
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			c := newClient()
			if err := c.GmailSend(to, subject, body, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Message sent.")
			return nil
		},
	}

	gmailReplyCmd := &cobra.Command{
		Use:   "reply <message-id>",
		Short: "Reply to a Gmail message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := cmd.Flags().GetString("body")
			account, _ := cmd.Flags().GetString("account")
			if body == "" {
				return fmt.Errorf("--body is required")
			}
			c := newClient()
			if err := c.GmailReply(args[0], body, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Reply sent.")
			return nil
		},
	}

	gmailListCmd.Flags().Bool("unread", false, "Filter to unread messages")
	gmailListCmd.Flags().String("query", "", "Gmail search query")
	gmailListCmd.Flags().Int("limit", 10, "Maximum number of messages to return")
	gmailListCmd.Flags().String("account", "default", "Account name")

	gmailReadCmd.Flags().String("account", "default", "Account name")

	gmailSendCmd.Flags().String("to", "", "Recipient email address (required)")
	gmailSendCmd.Flags().String("subject", "", "Email subject")
	gmailSendCmd.Flags().String("body", "", "Email body")
	gmailSendCmd.Flags().String("account", "default", "Account name")

	gmailReplyCmd.Flags().String("body", "", "Reply body (required)")
	gmailReplyCmd.Flags().String("account", "default", "Account name")

	gmailCmd.AddCommand(gmailListCmd, gmailReadCmd, gmailSendCmd, gmailReplyCmd)
	root.AddCommand(gmailCmd)
}
