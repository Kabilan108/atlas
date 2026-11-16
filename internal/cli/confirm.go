package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// confirmDeletion asks on the terminal before deleting what, unless --yes
// is set, and refuses without --yes when stdin is not a terminal. It
// reports false with a nil error when the user declines, after saying so.
func confirmDeletion(cmd *cobra.Command, what string) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("refusing to delete non-interactively without --yes")
	}
	fmt.Fprintf(os.Stderr, "Delete %s? [y/N] ", what)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	if answer := strings.ToLower(strings.TrimSpace(line)); answer == "y" || answer == "yes" {
		return true, nil
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), "Deletion cancelled.")
	return false, err
}
