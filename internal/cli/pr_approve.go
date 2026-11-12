package cli

import (
	"fmt"
	"io"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRApproveCmd() *cobra.Command {
	return newPRReviewActionCmd(pr.Approve, "approve [number|url|branch]", "Approve a pull request", "Approved pull request #%d\n")
}

func newPRRequestChangesCmd() *cobra.Command {
	return newPRReviewActionCmd(pr.RequestChanges, "request-changes [number|url|branch]", "Request changes on a pull request", "Requested changes on pull request #%d\n")
}

func newPRReviewActionCmd(op *ops.Op[pr.ApproveInput, *pr.ApprovalResult], use, short, done string) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.MaximumNArgs(1)}
	addPRRepoFlag(cmd)
	addCommentBodyFlags(cmd)
	return bind(cmd, binding[pr.ApproveInput, *pr.ApprovalResult]{
		op: op,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pr.ApproveInput, error) {
			body, _, err := readCommentBodyFromFlags(cmd, false)
			if err != nil {
				return pr.ApproveInput{}, err
			}
			ref, err := env.prRef(cmd, optionalArg(args))
			return pr.ApproveInput{Ref: ref, Body: body}, err
		},
		human: func(w io.Writer, result *pr.ApprovalResult) error {
			_, err := fmt.Fprintf(w, done, result.PullRequest())
			return err
		},
	})
}

func newPRUnapproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unapprove [number|url|branch]",
		Short: "Remove your approval from a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[pr.Ref, *pr.UnapproveResult]{
		op:    pr.Unapprove,
		input: prRefInput,
		human: func(w io.Writer, result *pr.UnapproveResult) error {
			_, err := fmt.Fprintf(w, "Removed approval from pull request #%d\n", result.PullRequest)
			return err
		},
	})
}

func newPRClearChangeRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear-change-request [number|url|branch]",
		Short: "Remove your change request from a pull request",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	return bind(cmd, binding[pr.Ref, *pr.ClearChangeRequestResult]{
		op:    pr.ClearChangeRequest,
		input: prRefInput,
		human: func(w io.Writer, result *pr.ClearChangeRequestResult) error {
			_, err := fmt.Fprintf(w, "Removed change request from pull request #%d\n", result.PullRequest)
			return err
		},
	})
}

func prRefInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.Ref, error) {
	return env.prRef(cmd, optionalArg(args))
}
