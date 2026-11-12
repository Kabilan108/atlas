package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const flagSnapshotPath = "testdata/flags.golden"

// Moving commands onto ops must not change their flags. The snapshot records
// every command's usage line, aliases, and flags (name, shorthand, type,
// default, and group annotations such as mutual exclusion). Run with -update
// after an intended change.
func TestFlagSnapshot(t *testing.T) {
	var b strings.Builder
	writeFlagSnapshot(&b, NewRootCmd("test"))
	got := b.String()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(flagSnapshotPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(flagSnapshotPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(flagSnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("CLI flags differ from %s (run with -update if intended)\n--- got ---\n%s", flagSnapshotPath, got)
	}
}

func writeFlagSnapshot(b *strings.Builder, cmd *cobra.Command) {
	fmt.Fprintf(b, "%s\n  use: %s\n", cmd.CommandPath(), cmd.Use)
	if len(cmd.Aliases) > 0 {
		fmt.Fprintf(b, "  aliases: %s\n", strings.Join(cmd.Aliases, ", "))
	}
	writeFlagSet(b, "persistent", cmd.PersistentFlags())
	writeFlagSet(b, "flag", cmd.Flags())
	for _, child := range cmd.Commands() {
		writeFlagSnapshot(b, child)
	}
}

func writeFlagSet(b *strings.Builder, kind string, flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		fmt.Fprintf(b, "  %s --%s", kind, f.Name)
		if f.Shorthand != "" {
			fmt.Fprintf(b, " -%s", f.Shorthand)
		}
		fmt.Fprintf(b, " %s default=%q", f.Value.Type(), f.DefValue)
		if f.Hidden {
			b.WriteString(" hidden")
		}
		keys := make([]string, 0, len(f.Annotations))
		for key := range f.Annotations {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values := append([]string(nil), f.Annotations[key]...)
			sort.Strings(values)
			fmt.Fprintf(b, " %s=%s", key, strings.Join(values, "|"))
		}
		b.WriteString("\n")
	})
}
