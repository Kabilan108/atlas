package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/output"
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
		RunE:  runSnippetList,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

func runSnippetList(cmd *cobra.Command, args []string) error {
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := workspaceFlag
	if workspace == "" {
		workspace = cfg.Workspace
	}
	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	snippets, err := client.ListSnippets(workspace)
	if err != nil {
		return err
	}

	if jsonOutput {
		return output.WriteJSON(os.Stdout, snippets)
	}

	if len(snippets) == 0 {
		fmt.Println("No snippets found.")
		return nil
	}

	tw := output.NewTableWriter(os.Stdout, "ID", "Title", "Files", "Visibility", "Updated")
	for _, s := range snippets {
		visibility := "public"
		if s.IsPrivate {
			visibility = "private"
		}
		tw.AddRow(
			s.ID,
			output.Truncate(s.Title, 40),
			fmt.Sprintf("%d", len(s.Files)),
			visibility,
			output.FormatRelativeTime(s.UpdatedOn),
		)
	}

	return tw.Flush()
}

func newSnippetViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "View a snippet",
		Args:  cobra.ExactArgs(1),
		RunE:  runSnippetView,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().Bool("contents", false, "Display file contents")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

type SnippetViewJSON struct {
	*bitbucket.Snippet
	FileContents map[string]string `json:"file_contents,omitempty"`
}

func runSnippetView(cmd *cobra.Command, args []string) error {
	snippetID := args[0]
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	showContents, _ := cmd.Flags().GetBool("contents")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := workspaceFlag
	if workspace == "" {
		workspace = cfg.Workspace
	}
	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	snippet, err := client.GetSnippet(workspace, snippetID)
	if err != nil {
		return err
	}

	if jsonOutput {
		result := SnippetViewJSON{Snippet: snippet}
		if showContents {
			result.FileContents = make(map[string]string)
			for filename := range snippet.Files {
				content, err := client.GetSnippetFileContent(workspace, snippetID, filename)
				if err != nil {
					return fmt.Errorf("failed to fetch file %s: %w", filename, err)
				}
				result.FileContents[filename] = string(content)
			}
		}
		return output.WriteJSON(os.Stdout, result)
	}

	visibility := "public"
	if snippet.IsPrivate {
		visibility = "private"
	}

	fmt.Printf("Title:      %s\n", snippet.Title)
	fmt.Printf("ID:         %s\n", snippet.ID)
	fmt.Printf("Owner:      %s\n", snippet.Owner.DisplayName)
	fmt.Printf("Visibility: %s\n", visibility)
	fmt.Printf("Created:    %s\n", output.FormatRelativeTime(snippet.CreatedOn))
	fmt.Printf("Updated:    %s\n", output.FormatRelativeTime(snippet.UpdatedOn))
	fmt.Printf("URL:        %s\n", snippet.Links.HTML.Href)
	fmt.Println()

	fmt.Printf("Files (%d):\n", len(snippet.Files))
	for filename := range snippet.Files {
		fmt.Printf("  - %s\n", filename)
	}

	if showContents {
		fmt.Println()
		for filename := range snippet.Files {
			content, err := client.GetSnippetFileContent(workspace, snippetID, filename)
			if err != nil {
				return fmt.Errorf("failed to fetch file %s: %w", filename, err)
			}
			fmt.Printf("=== %s ===\n", filename)
			fmt.Println(string(content))
		}
	}

	return nil
}

func newSnippetCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new snippet",
		RunE:  runSnippetCreate,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().String("title", "", "Snippet title")
	cmd.Flags().StringSliceP("file", "f", nil, "Files to include")
	cmd.Flags().Bool("private", true, "Make snippet private (visible to workspace members only)")
	cmd.Flags().Bool("json", false, "Output as JSON")
	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("file")

	return cmd
}

func runSnippetCreate(cmd *cobra.Command, args []string) error {
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	title, _ := cmd.Flags().GetString("title")
	files, _ := cmd.Flags().GetStringSlice("file")
	isPrivate, _ := cmd.Flags().GetBool("private")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := workspaceFlag
	if workspace == "" {
		workspace = cfg.Workspace
	}
	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}

	fileContents := make(map[string][]byte)
	for _, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		filename := filepath.Base(filePath)
		fileContents[filename] = content
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	snippet, err := client.CreateSnippet(workspace, title, fileContents, isPrivate)
	if err != nil {
		return err
	}

	if jsonOutput {
		return output.WriteJSON(os.Stdout, snippet)
	}

	fmt.Printf("Created snippet: %s\n", snippet.ID)
	fmt.Printf("URL: %s\n", snippet.Links.HTML.Href)

	return nil
}

func newSnippetUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a snippet",
		Args:  cobra.ExactArgs(1),
		RunE:  runSnippetUpdate,
	}

	cmd.Flags().String("workspace", "", "Target workspace")
	cmd.Flags().StringSliceP("file", "f", nil, "Files to add or update")
	cmd.Flags().StringSliceP("remove", "r", nil, "Files to remove")

	return cmd
}

func runSnippetUpdate(cmd *cobra.Command, args []string) error {
	snippetID := args[0]
	workspaceFlag, _ := cmd.Flags().GetString("workspace")
	files, _ := cmd.Flags().GetStringSlice("file")
	removeFiles, _ := cmd.Flags().GetStringSlice("remove")

	if len(files) == 0 && len(removeFiles) == 0 {
		return fmt.Errorf("at least one of --file or --remove must be specified")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := workspaceFlag
	if workspace == "" {
		workspace = cfg.Workspace
	}
	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --workspace")
	}

	fileContents := make(map[string][]byte)
	for _, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		filename := filepath.Base(filePath)
		fileContents[filename] = content
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	if err := client.UpdateSnippet(workspace, snippetID, fileContents, removeFiles); err != nil {
		return err
	}

	fmt.Printf("Updated snippet: %s\n", snippetID)

	return nil
}

func newSnippetDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a snippet",
		Args:  cobra.ExactArgs(1),
		RunE:  runSnippetDelete,
	}
}

func runSnippetDelete(cmd *cobra.Command, args []string) error {
	snippetID := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := cfg.Workspace
	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>'")
	}

	client, err := bitbucket.NewClient(bitbucket.WithNoCache(noCache))
	if err != nil {
		return err
	}

	if err := client.DeleteSnippet(workspace, snippetID); err != nil {
		return err
	}

	fmt.Printf("Deleted snippet: %s\n", snippetID)

	return nil
}
