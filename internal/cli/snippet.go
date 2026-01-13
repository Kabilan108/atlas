package cli

import (
	"github.com/spf13/cobra"
)

func newSnippetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snippet",
		Short: "Work with snippets",
	}

	cmd.AddCommand(newSnippetListCmd())
	cmd.AddCommand(newSnippetViewCmd())
	cmd.AddCommand(newSnippetCreateCmd())
	cmd.AddCommand(newSnippetUpdateCmd())
	cmd.AddCommand(newSnippetDeleteCmd())

	return cmd
}

func newSnippetListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your snippets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().String("workspace", "", "Target workspace")

	return cmd
}

func newSnippetViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "View a snippet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().Bool("contents", false, "Display file contents")

	return cmd
}

func newSnippetCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new snippet",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().String("title", "", "Snippet title")
	cmd.Flags().StringSliceP("file", "f", nil, "Files to include")
	cmd.Flags().Bool("private", true, "Make snippet private (visible to workspace members only)")
	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("file")

	return cmd
}

func newSnippetUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a snippet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().StringSliceP("file", "f", nil, "Files to add or update")
	cmd.Flags().StringSliceP("remove", "r", nil, "Files to remove")

	return cmd
}

func newSnippetDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a snippet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}
