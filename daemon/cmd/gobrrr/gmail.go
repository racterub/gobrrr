package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Gmail flag-value vars (Phase 2 will eliminate these).
var (
	gmailListUnread   bool
	gmailListQuery    string
	gmailListLimit    int
	gmailListAccount  string
	gmailReadAccount  string
	gmailSendTo       string
	gmailSendSubject  string
	gmailSendBody     string
	gmailSendAccount  string
	gmailReplyBody    string
	gmailReplyAccount string
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
			query := gmailListQuery
			if gmailListUnread && query == "" {
				query = "is:unread"
			} else if gmailListUnread {
				query = "is:unread " + query
			}
			c := newClient()
			result, err := c.GmailList(query, gmailListLimit, gmailListAccount, os.Getenv("GOBRRR_TASK_ID"))
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
			c := newClient()
			result, err := c.GmailRead(args[0], gmailReadAccount, os.Getenv("GOBRRR_TASK_ID"))
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
			if gmailSendTo == "" {
				return fmt.Errorf("--to is required")
			}
			c := newClient()
			if err := c.GmailSend(gmailSendTo, gmailSendSubject, gmailSendBody, gmailSendAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
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
			if gmailReplyBody == "" {
				return fmt.Errorf("--body is required")
			}
			c := newClient()
			if err := c.GmailReply(args[0], gmailReplyBody, gmailReplyAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Reply sent.")
			return nil
		},
	}

	gmailListCmd.Flags().BoolVar(&gmailListUnread, "unread", false, "Filter to unread messages")
	gmailListCmd.Flags().StringVar(&gmailListQuery, "query", "", "Gmail search query")
	gmailListCmd.Flags().IntVar(&gmailListLimit, "limit", 10, "Maximum number of messages to return")
	gmailListCmd.Flags().StringVar(&gmailListAccount, "account", "default", "Account name")

	gmailReadCmd.Flags().StringVar(&gmailReadAccount, "account", "default", "Account name")

	gmailSendCmd.Flags().StringVar(&gmailSendTo, "to", "", "Recipient email address (required)")
	gmailSendCmd.Flags().StringVar(&gmailSendSubject, "subject", "", "Email subject")
	gmailSendCmd.Flags().StringVar(&gmailSendBody, "body", "", "Email body")
	gmailSendCmd.Flags().StringVar(&gmailSendAccount, "account", "default", "Account name")

	gmailReplyCmd.Flags().StringVar(&gmailReplyBody, "body", "", "Reply body (required)")
	gmailReplyCmd.Flags().StringVar(&gmailReplyAccount, "account", "default", "Account name")

	gmailCmd.AddCommand(gmailListCmd, gmailReadCmd, gmailSendCmd, gmailReplyCmd)
	root.AddCommand(gmailCmd)
}
