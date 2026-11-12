package cli

import (
	"io"
	"os"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// binding wires an op to a cobra command. The command defines its own
// positional args and input flags; bind adds the output flags and runs it.
type binding[In, Out any] struct {
	op *ops.Op[In, Out]
	// input builds the op input from the command's args and flags.
	input func(cmd *cobra.Command, args []string, env *cliEnv) (In, error)
	// intercept, if set, may handle the command itself (for example --web)
	// and report true so the op does not run.
	intercept func(cmd *cobra.Command, env *cliEnv, in In) (bool, error)
	// pagerName names the markdown file the pager shows.
	pagerName func(Out) string
}

// bind adds --json, plus --raw for ops that render markdown, and sets RunE.
// Markdown goes through the pager on a terminal unless --raw is set; JSON and
// piped output go straight to the command's stdout.
func bind[In, Out any](cmd *cobra.Command, b binding[In, Out]) *cobra.Command {
	cmd.Flags().Bool("json", false, "Output as JSON")
	if b.op.Markdown != nil {
		cmd.Flags().BoolP("raw", "r", false, "Print raw markdown to stdout")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		env, err := newCLIEnv()
		if err != nil {
			return err
		}
		in, err := b.input(cmd, args, env)
		if err != nil {
			return err
		}
		if b.intercept != nil {
			if handled, err := b.intercept(cmd, env, in); handled || err != nil {
				return err
			}
		}
		out, err := b.op.Call(cmd.Context(), env, in)
		if err != nil {
			return err
		}
		return b.write(cmd, out)
	}
	return cmd
}

func (b binding[In, Out]) write(cmd *cobra.Command, out Out) error {
	stdout := cmd.OutOrStdout()
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput || b.op.Markdown == nil {
		return output.WriteJSON(stdout, out)
	}
	markdown, err := b.op.Markdown(out)
	if err != nil {
		return err
	}
	rawOutput, _ := cmd.Flags().GetBool("raw")
	if !rawOutput && isTerminal(stdout) {
		name := b.op.Name + ".md"
		if b.pagerName != nil {
			name = b.pagerName(out)
		}
		return pageMarkdown([]byte(markdown), name)
	}
	_, err = io.WriteString(stdout, markdown)
	return err
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
