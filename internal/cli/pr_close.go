package cli

import (
	"fmt"
	"io"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/spf13/cobra"
)

func newPRCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close <number|url|branch>",
		Short: "Close a pull request",
		Args:  cobra.ExactArgs(1),
	}
	addPRRepoFlag(cmd)
	cmd.Flags().StringP("comment", "c", "", "Leave a closing comment")
	return bind(cmd, binding[pr.CloseInput, *bitbucket.PullRequest]{
		op: pr.Close,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pr.CloseInput, error) {
			comment, _ := cmd.Flags().GetString("comment")
			ref, err := env.prRef(cmd, args[0])
			return pr.CloseInput{Ref: ref, Comment: comment}, err
		},
		human: func(w io.Writer, closed *bitbucket.PullRequest) error {
			_, err := fmt.Fprintf(w, "Closed pull request #%d: %s\n", closed.ID, closed.Title)
			return err
		},
	})
}
