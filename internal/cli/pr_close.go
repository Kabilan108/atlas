package cli

import (
	"fmt"
	"io"

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
	cmd.Flags().StringP("body", "b", "", "Leave a closing comment")
	return bind(cmd, binding[pr.CloseInput, *pr.CloseResult]{
		op: pr.Close,
		input: func(cmd *cobra.Command, args []string, env *cliEnv) (pr.CloseInput, error) {
			body, _ := cmd.Flags().GetString("body")
			ref, err := env.requiredPRRef(cmd, args[0])
			return pr.CloseInput{Ref: ref, Body: body}, err
		},
		human: func(w io.Writer, closed *pr.CloseResult) error {
			if closed.Failed != nil {
				return printFailedStep(w, closed.Failed)
			}
			_, err := fmt.Fprintf(w, "Closed pull request #%d: %s\n", closed.ID, closed.Title)
			return err
		},
	})
}
