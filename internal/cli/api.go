package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/ops/api"
	"github.com/spf13/cobra"
)

func newAPICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "GET a Bitbucket REST API path and print the response",
		Long: `GET a Bitbucket REST API path, such as /2.0/user or
/2.0/repositories/acme/app/refs/tags?pagelen=50, and print the response body.

Only GET is supported. Prefer the dedicated commands where one exists.`,
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().Int("max-bytes", api.HardMaxBytes, "Response byte cap (at most 4194304)")
	return bind(cmd, binding[api.GetInput, *api.GetResult]{
		op: api.Get,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (api.GetInput, error) {
			maxBytes, _ := cmd.Flags().GetInt("max-bytes")
			return api.GetInput{Path: args[0], MaxBytes: maxBytes}, nil
		},
		human: func(w io.Writer, out *api.GetResult) error {
			body := out.Body
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			if _, err := io.WriteString(w, body); err != nil {
				return err
			}
			if out.Truncated {
				fmt.Fprintf(cmd.ErrOrStderr(), "Response truncated at %d bytes (--max-bytes)\n", len(out.Body))
			}
			return nil
		},
	})
}
