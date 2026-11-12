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
	cmd.Flags().String("state", "open", "Filter by state: open, merged, declined, superseded")
	cmd.Flags().String("author", "", "Filter by author username")
	cmd.Flags().String("reviewer", "", "Filter by reviewer username")
	return bind(cmd, binding[pr.ListInput, *pr.ListResult]{
		op:    pr.List,
		input: prListInput,
		human: func(w io.Writer, result *pr.ListResult) error { return printPRTable(w, result.PullRequests) },
	})
}

func prListInput(cmd *cobra.Command, _ []string, env *cliEnv) (pr.ListInput, error) {
	allRepos, _ := cmd.Flags().GetBool("all")
	stateFlag, _ := cmd.Flags().GetString("state")
	author, _ := cmd.Flags().GetString("author")
	reviewer, _ := cmd.Flags().GetString("reviewer")
	var repo string
	var err error
	if allRepos {
		repo, err = env.workspaceScope(cmd)
	} else {
		repo, err = env.repository(cmd)
	}
	if err != nil {
		return pr.ListInput{}, err
	}
	state, err := pr.ParseState(stateFlag)
	if err != nil {
		return pr.ListInput{}, err
	}
	return pr.ListInput{RepoRef: ops.RepoRef{Repo: repo}, State: state, Author: author, Reviewer: reviewer}, nil
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
