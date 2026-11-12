package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestFlagOrEnvPrecedence(t *testing.T) {
	newCmd := func(args ...string) *cobra.Command {
		cmd := &cobra.Command{Use: "x"}
		cmd.Flags().String("opt", "", "")
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	t.Setenv("ATLAS_TEST_OPT", "")
	if got := flagOrEnv(newCmd(), "opt", "ATLAS_TEST_OPT", "fallback"); got != "fallback" {
		t.Fatalf("default = %q", got)
	}
	t.Setenv("ATLAS_TEST_OPT", "from-env")
	if got := flagOrEnv(newCmd(), "opt", "ATLAS_TEST_OPT", "fallback"); got != "from-env" {
		t.Fatalf("env = %q", got)
	}
	if got := flagOrEnv(newCmd("--opt", "from-flag"), "opt", "ATLAS_TEST_OPT", "fallback"); got != "from-flag" {
		t.Fatalf("flag = %q", got)
	}
}
