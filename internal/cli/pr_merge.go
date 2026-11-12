package cli

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge [number|url|branch]",
		Short: "Merge a pull request",
		Long:  "Merge a pull request and wait for Bitbucket to finish.\nExits non-zero when the merge is still running at --wait; it is not retried.",
		Args:  cobra.MaximumNArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().String("strategy", "", "Merge strategy: "+strings.Join(pr.MergeStrategy("").Enum(), ", ")+" (default: the destination branch's)")
	cmd.Flags().String("message", "", "Merge commit message")
	cmd.Flags().Bool("close-source-branch", false, "Delete the source branch after merging (default: the PR's setting)")
	cmd.Flags().Duration("wait", 0, "How long to wait for a queued merge (default 2m, max 10m)")
	return bind(cmd, binding[pr.MergeInput, *pr.MergeReceipt]{
		op:    pr.Merge,
		input: prMergeInput,
		after: func(_ *cobra.Command, receipt *pr.MergeReceipt) error {
			if receipt.State == pr.MergePending {
				return fmt.Errorf("merge of PR #%d is still running; check 'atlas pr view %d' before retrying", receipt.PR, receipt.PR)
			}
			return nil
		},
	})
}

func prMergeInput(cmd *cobra.Command, args []string, env *cliEnv) (pr.MergeInput, error) {
	ref, err := env.prRef(cmd, optionalArg(args))
	if err != nil {
		return pr.MergeInput{}, err
	}
	strategy, _ := cmd.Flags().GetString("strategy")
	message, _ := cmd.Flags().GetString("message")
	wait, _ := cmd.Flags().GetDuration("wait")
	seconds, err := waitSeconds(wait)
	if err != nil {
		return pr.MergeInput{}, err
	}
	in := pr.MergeInput{Ref: ref, Strategy: pr.MergeStrategy(strategy), Message: message, WaitSeconds: seconds}
	if cmd.Flags().Changed("close-source-branch") {
		closeBranch, _ := cmd.Flags().GetBool("close-source-branch")
		in.CloseSourceBranch = &closeBranch
	}
	return in, nil
}

// waitSeconds turns a --wait duration into whole seconds, rounding up.
func waitSeconds(wait time.Duration) (int, error) {
	if wait < 0 {
		return 0, fmt.Errorf("--wait must not be negative")
	}
	return int(math.Ceil(wait.Seconds())), nil
}
