package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
)

func newPRListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
	}
	addPRRepoFlag(cmd)
	cmd.Flags().Bool("all", false, "List PRs across all repos in workspace")
	cmd.Flags().StringSlice("repos", nil, "List PRs in these repositories instead (comma-separated or repeated, up to 50)")
	cmd.Flags().String("state", "open", "Filter by state: open, merged, declined, superseded, all")
	cmd.Flags().String("author", "", "Filter by author username")
	cmd.Flags().String("reviewer", "", "Filter by reviewer username")
	cmd.Flags().String("updated-since", "", "Only PRs updated on or after this date (YYYY-MM-DD or ISO 8601)")
	cmd.Flags().IntP("limit", "L", pr.DefaultListLimit, "Maximum PRs per repository, newest updated first (max 500)")
	return bind(cmd, binding[pr.ListInput, *pr.ListResult]{
		op:    pr.List,
		input: prListInput,
		human: func(w io.Writer, result *pr.ListResult) error { return printPRTable(w, result.PullRequests) },
	})
}

func prListInput(cmd *cobra.Command, _ []string, env *cliEnv) (pr.ListInput, error) {
	allRepos, _ := cmd.Flags().GetBool("all")
	repos, _ := cmd.Flags().GetStringSlice("repos")
	stateFlag, _ := cmd.Flags().GetString("state")
	author, _ := cmd.Flags().GetString("author")
	reviewer, _ := cmd.Flags().GetString("reviewer")
	updatedSince, _ := cmd.Flags().GetString("updated-since")
	limit, _ := cmd.Flags().GetInt("limit")
	state, err := pr.ParseState(stateFlag)
	if err != nil {
		return pr.ListInput{}, err
	}
	in := pr.ListInput{Repos: repos, State: state, Author: author, Reviewer: reviewer, UpdatedSince: updatedSince, Limit: limit}
	switch {
	case len(repos) > 0 && allRepos:
		return pr.ListInput{}, fmt.Errorf("use --all or --repos, not both")
	case len(repos) > 0:
		if repoFlag, _ := cmd.Flags().GetString("repo"); repoFlag != "" {
			return pr.ListInput{}, fmt.Errorf("use -R or --repos, not both")
		}
		return in, nil
	case allRepos:
		in.Repo, err = env.workspaceScope(cmd)
	default:
		in.Repo, err = env.repository(cmd)
	}
	if err != nil {
		return pr.ListInput{}, err
	}
	return in, nil
}

// workspaceScope is "workspace/*" for the workspace -R names, or the
// configured one. Unlike repository it never asks git, since --all covers
// every repository rather than the checkout's.
func (e *cliEnv) workspaceScope(cmd *cobra.Command) (string, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	workspace := e.cfg.Workspace
	if repoFlag != "" {
		parts := strings.Split(repoFlag, "/")
		switch len(parts) {
		case 1:
		case 2:
			workspace = parts[0]
		default:
			return "", fmt.Errorf("invalid repository %q: expected repo or workspace/repo", repoFlag)
		}
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace not configured. Run 'atlas config set workspace <name>' or use -R workspace/repo")
	}
	return workspace + "/" + ops.AllRepos, nil
}

func printPRTable(w io.Writer, prs []bitbucket.PullRequest) error {
	if len(prs) == 0 {
		_, err := fmt.Fprintln(w, "No pull requests found.")
		return err
	}
	hasComments := false
	for _, pr := range prs {
		if pr.CommentCount > 0 || pr.TaskCount > 0 {
			hasComments = true
			break
		}
	}
	headers := []string{"ID", "Title", "Author", "State", "Updated"}
	if hasComments {
		headers = append(headers, "Feedback")
	}
	if isTerminal(w) {
		for i, header := range headers {
			headers[i] = styleBold(header)
		}
	}
	tw := output.NewTableWriter(w, headers...)
	for _, p := range prs {
		row := []string{
			stylePRID(p.ID),
			output.Truncate(p.Title, 50),
			p.Author.DisplayName,
			stylePRState(p),
			output.FormatRelativeTime(p.UpdatedOn),
		}
		if hasComments {
			row = append(row, pr.FeedbackSummary(p))
		}
		tw.AddRow(row...)
	}
	return tw.Flush()
}
