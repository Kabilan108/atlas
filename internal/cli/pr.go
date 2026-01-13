package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
)

func newPRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Work with pull requests",
	}

	cmd.AddCommand(newPRListCmd())
	cmd.AddCommand(newPRViewCmd())
	cmd.AddCommand(newPRCheckoutCmd())

	return cmd
}

func newPRListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE:  runPRList,
	}

	cmd.Flags().String("repo", "", "Target repository")
	cmd.Flags().Bool("all", false, "List PRs across all repos in workspace")
	cmd.Flags().String("state", "open", "Filter by state: open, merged, declined, superseded")
	cmd.Flags().String("author", "", "Filter by author username")
	cmd.Flags().String("reviewer", "", "Filter by reviewer username")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

func runPRList(cmd *cobra.Command, args []string) error {
	allRepos, _ := cmd.Flags().GetBool("all")
	repoFlag, _ := cmd.Flags().GetString("repo")
	state, _ := cmd.Flags().GetString("state")
	author, _ := cmd.Flags().GetString("author")
	reviewer, _ := cmd.Flags().GetString("reviewer")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := cfg.Workspace
	repo := repoFlag

	if !allRepos && repo == "" {
		inferredWS, inferredRepo, err := git.InferRepository()
		if err != nil {
			return fmt.Errorf("could not infer repository: %w\nUse --repo to specify or --all for all repos", err)
		}
		if workspace == "" {
			workspace = inferredWS
		}
		repo = inferredRepo
		if verbose {
			fmt.Fprintf(os.Stderr, "Using repository: %s/%s\n", workspace, repo)
		}
	}

	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use --all")
	}

	client, err := bitbucket.NewClient(
		bitbucket.WithNoCache(noCache),
	)
	if err != nil {
		return err
	}

	opts := &bitbucket.PRListOptions{
		State:    strings.ToUpper(state),
		Author:   author,
		Reviewer: reviewer,
	}

	var prs []bitbucket.PullRequest
	if allRepos {
		prs, err = client.ListAllPullRequests(workspace, opts)
	} else {
		prs, err = client.ListPullRequests(workspace, repo, opts)
	}
	if err != nil {
		return err
	}

	if jsonOutput {
		return output.WriteJSON(os.Stdout, prs)
	}

	if len(prs) == 0 {
		fmt.Println("No pull requests found.")
		return nil
	}

	hasComments := false
	for _, pr := range prs {
		if pr.CommentCount > 0 {
			hasComments = true
			break
		}
	}

	headers := []string{"ID", "Title", "Author", "State", "Updated"}
	if hasComments {
		headers = append(headers, "Comments")
	}

	tw := output.NewTableWriter(os.Stdout, headers...)
	for _, pr := range prs {
		row := []string{
			fmt.Sprintf("#%d", pr.ID),
			output.Truncate(pr.Title, 50),
			pr.Author.DisplayName,
			pr.State,
			output.FormatRelativeTime(pr.UpdatedOn),
		}
		if hasComments {
			row = append(row, fmt.Sprintf("%d", pr.CommentCount))
		}
		tw.AddRow(row...)
	}

	return tw.Flush()
}

func newPRViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <id|branch>",
		Short: "View a pull request",
		Args:  cobra.ExactArgs(1),
		RunE:  runPRView,
	}

	cmd.Flags().String("repo", "", "Target repository")
	cmd.Flags().Bool("comments", false, "Include all comments")
	cmd.Flags().Bool("all", false, "Include resolved comments (only with --comments)")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

type PRViewJSON struct {
	*bitbucket.PullRequest
	Comments []bitbucket.Comment `json:"comments,omitempty"`
}

func runPRView(cmd *cobra.Command, args []string) error {
	repoFlag, _ := cmd.Flags().GetString("repo")
	showComments, _ := cmd.Flags().GetBool("comments")
	includeResolved, _ := cmd.Flags().GetBool("all")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspace := cfg.Workspace
	repo := repoFlag

	if repo == "" {
		inferredWS, inferredRepo, err := git.InferRepository()
		if err != nil {
			return fmt.Errorf("could not infer repository: %w\nUse --repo to specify", err)
		}
		if workspace == "" {
			workspace = inferredWS
		}
		repo = inferredRepo
		if verbose {
			fmt.Fprintf(os.Stderr, "Using repository: %s/%s\n", workspace, repo)
		}
	}

	if workspace == "" {
		return fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>'")
	}

	client, err := bitbucket.NewClient(
		bitbucket.WithNoCache(noCache),
	)
	if err != nil {
		return err
	}

	pr, err := resolvePR(client, workspace, repo, args[0])
	if err != nil {
		return err
	}

	if jsonOutput {
		result := PRViewJSON{PullRequest: pr}
		comments, err := client.ListPullRequestComments(workspace, repo, pr.ID)
		if err != nil {
			return fmt.Errorf("failed to fetch comments: %w", err)
		}
		result.Comments = comments
		return output.WriteJSON(os.Stdout, result)
	}

	mdWriter := output.NewPRMarkdownWriter(os.Stdout)
	if err := mdWriter.WritePR(pr); err != nil {
		return err
	}

	if showComments {
		comments, err := client.ListPullRequestComments(workspace, repo, pr.ID)
		if err != nil {
			return fmt.Errorf("failed to fetch comments: %w", err)
		}

		diff, _ := client.GetPullRequestDiff(workspace, repo, pr.ID)

		fmt.Println()
		commentWriter := output.NewCommentWriter(os.Stdout, pr.Author.UUID)
		if len(diff) > 0 {
			commentWriter.SetDiff(diff)
		}
		if err := commentWriter.WriteComments(comments, includeResolved); err != nil {
			return err
		}

		tasks, err := client.ListPullRequestTasks(workspace, repo, pr.ID)
		if err != nil {
			return fmt.Errorf("failed to fetch tasks: %w", err)
		}

		if len(tasks) > 0 {
			fmt.Println()
			taskWriter := output.NewTaskWriter(os.Stdout)
			if err := taskWriter.WriteTasks(tasks); err != nil {
				return err
			}
		}
	}

	return nil
}

func resolvePR(client *bitbucket.Client, workspace, repo, ref string) (*bitbucket.PullRequest, error) {
	var prID int
	if _, err := fmt.Sscanf(ref, "%d", &prID); err == nil {
		return client.GetPullRequest(workspace, repo, prID)
	}

	ref = strings.TrimPrefix(ref, "#")
	if _, err := fmt.Sscanf(ref, "%d", &prID); err == nil {
		return client.GetPullRequest(workspace, repo, prID)
	}

	return client.FindPullRequestByBranch(workspace, repo, ref)
}

func newPRCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout <id|branch>",
		Short: "Checkout a PR branch locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().String("repo", "", "Target repository")

	return cmd
}
