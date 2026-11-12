package cli

import (
	"errors"
	"io"
	"os"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// opAnnotation is the cobra annotation naming the op a command is bound to.
const opAnnotation = "op"

// binding wires an op to a cobra command. The command defines its own
// positional args and input flags; bind adds the output flags and runs it.
// Everything CLI-only about presenting the result lives here, not in the op.
type binding[In, Out any] struct {
	op *ops.Op[In, Out]
	// input builds the op input from the command's args and flags.
	input func(cmd *cobra.Command, args []string, env *cliEnv) (In, error)
	// intercept, if set, may handle the command instead of the op (for
	// example view --web, which opens the browser without fetching what the
	// op would print) and reports true so the op does not run.
	intercept func(cmd *cobra.Command, env *cliEnv, in In) (bool, error)
	// human writes Out for people when --json is not set. Without it the
	// command prints the op's markdown, or JSON when the op has none.
	human func(w io.Writer, out Out) error
	// pager names the file a terminal pager shows the op's markdown as.
	// Setting it adds --raw; without it markdown is always written as is.
	pager func(Out) string
	// after runs once the output is written and the op fully succeeded, for
	// example to open a created PR with --web or to fail on failed checks.
	after func(cmd *cobra.Command, out Out) error
}

// bind adds --json (and --raw with a pager), tags the command with the op's
// name, and sets RunE. A Partial error is returned after the output it came
// with is written, so the command prints what it got and still fails.
func bind[In, Out any](cmd *cobra.Command, b binding[In, Out]) *cobra.Command {
	cmd.Flags().Bool("json", false, "Output as JSON")
	if b.pager != nil {
		cmd.Flags().BoolP("raw", "r", false, "Print raw markdown to stdout")
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[opAnnotation] = b.op.Name
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Flag and argument errors are reported before RunE with usage; an
		// error from here on is about the request, so usage would be noise.
		cmd.SilenceUsage = true
		env, err := newCLIEnv(cmd)
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
		out, runErr := b.op.Call(cmd.Context(), env, in)
		if runErr != nil && ops.KindOf(runErr) != ops.Partial {
			return runErr
		}
		if err := b.write(cmd, out); err != nil {
			return errors.Join(runErr, err)
		}
		if runErr != nil || b.after == nil {
			return runErr
		}
		return b.after(cmd, out)
	}
	return cmd
}

func (b binding[In, Out]) write(cmd *cobra.Command, out Out) error {
	stdout := cmd.OutOrStdout()
	if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
		return output.WriteJSON(stdout, out)
	}
	if b.human != nil {
		return b.human(stdout, out)
	}
	if b.op.Markdown == nil {
		return output.WriteJSON(stdout, out)
	}
	markdown, err := b.op.Markdown(out)
	if err != nil {
		return err
	}
	if b.pager != nil {
		if raw, _ := cmd.Flags().GetBool("raw"); !raw && isTerminal(stdout) {
			return pageMarkdown([]byte(markdown), b.pager(out))
		}
	}
	_, err = io.WriteString(stdout, markdown)
	return err
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
