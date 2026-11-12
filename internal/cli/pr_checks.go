package cli

import (
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRChecksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checks [number|url|branch]",
		Short: "Show build statuses on a pull request's head commit",
		Long:  "Show build statuses on a pull request's head commit, or on --commit.\nExits non-zero when a check failed or is still running.",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("commit", "", "Read this commit's statuses instead of the PR head")
	return bind(cmd, binding[pr.ChecksInput, *pr.ChecksResult]{
		op:    pr.Checks,
		input: prChecksInput,
		after: failOnChecks,
	})
}

func prChecksInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.ChecksInput, error) {
	commit, _ := cmd.Flags().GetString("commit")
	if commit != "" && len(args) == 0 {
		repo, err := env.repository(cmd)
		if err != nil {
			return pr.ChecksInput{}, err
		}
		return pr.ChecksInput{RepoRef: ops.RepoRef{Repo: repo}, Commit: commit}, nil
	}
	ref, err := env.prRef(cmd, optionalArg(args))
	if err != nil {
		return pr.ChecksInput{}, err
	}
	return pr.ChecksInput{RepoRef: ref.RepoRef, PR: ref.PR, CurrentBranch: ref.CurrentBranch, Commit: commit}, nil
}

func failOnChecks(_ *cobra.Command, result *pr.ChecksResult) error {
	switch result.Overall {
	case pr.ChecksFailed:
		var names []string
		for _, check := range result.Failing() {
			names = append(names, fmt.Sprintf("%s (%s)", check.Name, check.State))
		}
		return fmt.Errorf("checks failed: %s", strings.Join(names, ", "))
	case pr.ChecksPending:
		return fmt.Errorf("checks are still running")
	}
	return nil
}
