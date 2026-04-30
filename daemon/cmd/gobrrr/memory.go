package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/memory"
)

// registerMemory wires the `memory` verb (save/search/list/get/delete) onto root.
func registerMemory(root *cobra.Command) {
	memoryCmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage daemon memory",
	}

	memorySaveCmd := &cobra.Command{
		Use:   "save",
		Short: "Save a memory entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, _ := cmd.Flags().GetString("content")
			tagsStr, _ := cmd.Flags().GetString("tags")
			source, _ := cmd.Flags().GetString("source")
			if content == "" {
				return fmt.Errorf("--content is required")
			}
			var tags []string
			if tagsStr != "" {
				for _, t := range strings.Split(tagsStr, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
			c := newClient()
			entry, err := c.SaveMemory(content, tags, source)
			if err != nil {
				return err
			}
			printMemoryEntry(entry)
			return nil
		},
	}

	memorySearchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memory entries",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			tagsStr, _ := cmd.Flags().GetString("tags")
			var tags []string
			if tagsStr != "" {
				for _, t := range strings.Split(tagsStr, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
			c := newClient()
			entries, err := c.SearchMemory(query, tags, 0)
			if err != nil {
				return err
			}
			printMemoryList(entries)
			return nil
		},
	}

	memoryListCmd := &cobra.Command{
		Use:   "list",
		Short: "List memory entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			c := newClient()
			entries, err := c.SearchMemory("", nil, limit)
			if err != nil {
				return err
			}
			printMemoryList(entries)
			return nil
		},
	}

	memoryGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a memory entry by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			entry, err := c.GetMemory(args[0])
			if err != nil {
				return err
			}
			printMemoryEntry(entry)
			return nil
		},
	}

	memoryDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a memory entry by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.DeleteMemory(args[0]); err != nil {
				return err
			}
			fmt.Printf("Memory %s deleted.\n", args[0])
			return nil
		},
	}

	memorySaveCmd.Flags().String("content", "", "Memory content (required)")
	memorySaveCmd.Flags().String("tags", "", "Comma-separated tags")
	memorySaveCmd.Flags().String("source", "", "Source of the memory")

	memorySearchCmd.Flags().String("tags", "", "Comma-separated tags to filter by")

	memoryListCmd.Flags().Int("limit", 20, "Maximum number of entries to return")

	memoryCmd.AddCommand(memorySaveCmd, memorySearchCmd, memoryListCmd, memoryGetCmd, memoryDeleteCmd)
	root.AddCommand(memoryCmd)
}

// printMemoryEntry prints full details of a memory entry.
func printMemoryEntry(e *memory.Entry) {
	fmt.Printf("ID:         %s\n", e.ID)
	fmt.Printf("Source:     %s\n", e.Source)
	fmt.Printf("Tags:       %s\n", strings.Join(e.Tags, ", "))
	fmt.Printf("Created:    %s\n", e.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Content:    %s\n", e.Content)
}

// printMemoryList prints a compact list of memory entries.
func printMemoryList(entries []*memory.Entry) {
	if len(entries) == 0 {
		fmt.Println("No memory entries.")
		return
	}
	for _, e := range entries {
		summary := e.Content
		if len(summary) > 60 {
			summary = summary[:60] + "..."
		}
		fmt.Printf("%-30s  %-20s  %s\n", e.ID, strings.Join(e.Tags, ","), summary)
	}
}
