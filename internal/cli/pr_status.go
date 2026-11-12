package cli

import (
	"fmt"
	"io"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/git"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of relevant pull requests",
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[pr.StatusInput, *pr.StatusResult]{
		op: pr.Status,
		input: func(cmd *cobra.Command, _ []string, env *cliEnv) (pr.StatusInput, error) {
			repo, err := env.repository(cmd)
			if err != nil {
				return pr.StatusInput{}, err
			}
			branch, _ := git.CurrentBranch()
			return pr.StatusInput{RepoRef: ops.RepoRef{Repo: repo}, Branch: branch}, nil
		},
		human: printPRStatus,
	})
}

func printPRStatus(w io.Writer, status *pr.StatusResult) error {
	fmt.Fprintln(w, "Current branch")
	if status.CurrentBranch == nil {
		fmt.Fprintln(w, "  "+styleMuted("No pull request"))
	} else {
		printStatusPR(w, *status.CurrentBranch)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Created by you")
	printStatusPRs(w, status.CreatedByYou)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Requesting your review")
	printStatusPRs(w, status.ReviewRequested)
	return nil
}

func printStatusPRs(w io.Writer, prs []bitbucket.PullRequest) {
	if len(prs) == 0 {
		fmt.Fprintln(w, "  "+styleMuted("None"))
		return
	}
	for _, p := range prs {
		printStatusPR(w, p)
	}
}

func printStatusPR(w io.Writer, p bitbucket.PullRequest) {
	feedback := pr.FeedbackSummary(p)
	if feedback != "" {
		feedback = " " + styleMuted(feedback)
	}
	fmt.Fprintf(w, "  %s %s %s %s%s\n", stylePRID(p.ID), p.Title, styleMuted("["+p.Source.Branch.Name+"]"), reviewerSummary(p), feedback)
}
