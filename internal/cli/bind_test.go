package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/spf13/cobra"
)

type fakeIn struct {
	Items []string `json:"items"`
}

type fakeOut struct {
	Done []string `json:"done"`
}

// fakeOp finishes the first item; with more it returns a Partial error, and
// "fail" fails outright.
func fakeOp(markdown bool) *ops.Op[fakeIn, fakeOut] {
	op := &ops.Op[fakeIn, fakeOut]{
		Meta: ops.Meta{Name: "fake_op", Surfaces: ops.Both},
		Run: func(_ context.Context, _ ops.Env, in fakeIn) (fakeOut, error) {
			if len(in.Items) > 0 && in.Items[0] == "fail" {
				return fakeOut{}, ops.Errorf(ops.Upstream, "boom")
			}
			if len(in.Items) > 1 {
				return fakeOut{Done: in.Items[:1]}, ops.Errorf(ops.Partial, "1 of %d done", len(in.Items))
			}
			return fakeOut{Done: in.Items}, nil
		},
	}
	if markdown {
		op.Markdown = func(out fakeOut) (string, error) { return "# " + strings.Join(out.Done, ",") + "\n", nil }
	}
	return op
}

func runBound(t *testing.T, b binding[fakeIn, fakeOut], args ...string) (*cobra.Command, string, error) {
	t.Helper()
	setupCLIEnv(t)
	cmd := bind(&cobra.Command{Use: "fake", SilenceUsage: true, SilenceErrors: true}, b)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return cmd, stdout.String(), err
}

func itemsInput(_ *cobra.Command, args []string, _ *cliEnv) (fakeIn, error) {
	return fakeIn{Items: args}, nil
}

func TestBindWritesPartialOutputThenFails(t *testing.T) {
	_, stdout, err := runBound(t, binding[fakeIn, fakeOut]{op: fakeOp(true), input: itemsInput}, "a", "b")
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want partial", err)
	}
	if stdout != "# a\n" {
		t.Fatalf("stdout = %q, want the partial result", stdout)
	}
}

func TestBindWritesNothingOnFailure(t *testing.T) {
	afterRan := false
	_, stdout, err := runBound(t, binding[fakeIn, fakeOut]{
		op:    fakeOp(true),
		input: itemsInput,
		after: func(*cobra.Command, fakeOut) error { afterRan = true; return nil },
	}, "fail")
	if err == nil || stdout != "" || afterRan {
		t.Fatalf("err = %v, stdout = %q, after ran = %v", err, stdout, afterRan)
	}
}

func TestBindOutputChoices(t *testing.T) {
	human := func(w io.Writer, out fakeOut) error {
		_, err := fmt.Fprintf(w, "done: %s\n", strings.Join(out.Done, " "))
		return err
	}
	tests := []struct {
		name string
		b    binding[fakeIn, fakeOut]
		args []string
		want string
	}{
		{"markdown", binding[fakeIn, fakeOut]{op: fakeOp(true)}, []string{"a"}, "# a\n"},
		{"json without markdown", binding[fakeIn, fakeOut]{op: fakeOp(false)}, []string{"a"}, "{\n  \"done\": [\n    \"a\"\n  ]\n}\n"},
		{"human", binding[fakeIn, fakeOut]{op: fakeOp(true), human: human}, []string{"a"}, "done: a\n"},
		{"json beats human", binding[fakeIn, fakeOut]{op: fakeOp(true), human: human}, []string{"a", "--json"}, "{\n  \"done\": [\n    \"a\"\n  ]\n}\n"},
		{"raw with pager off a terminal", binding[fakeIn, fakeOut]{op: fakeOp(true), pager: func(fakeOut) string { return "x.md" }}, []string{"a", "--raw"}, "# a\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.b.input = func(_ *cobra.Command, args []string, _ *cliEnv) (fakeIn, error) {
				return fakeIn{Items: args}, nil
			}
			_, stdout, err := runBound(t, tc.b, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if stdout != tc.want {
				t.Fatalf("stdout = %q, want %q", stdout, tc.want)
			}
		})
	}
}

func TestBindRawFlagFollowsPager(t *testing.T) {
	with := bind(&cobra.Command{Use: "a"}, binding[fakeIn, fakeOut]{op: fakeOp(true), pager: func(fakeOut) string { return "a.md" }})
	without := bind(&cobra.Command{Use: "b"}, binding[fakeIn, fakeOut]{op: fakeOp(true)})
	if with.Flags().Lookup("raw") == nil || without.Flags().Lookup("raw") != nil {
		t.Fatal("--raw must exist exactly when the binding has a pager")
	}
	if with.Annotations[opAnnotation] != "fake_op" {
		t.Fatalf("annotations = %v", with.Annotations)
	}
}

func TestBindAfterRunsOnSuccessAndItsErrorFails(t *testing.T) {
	failed := errors.New("checks failed")
	var seen []string
	_, stdout, err := runBound(t, binding[fakeIn, fakeOut]{
		op:    fakeOp(true),
		input: itemsInput,
		after: func(_ *cobra.Command, out fakeOut) error { seen = out.Done; return failed },
	}, "a")
	if !errors.Is(err, failed) || stdout != "# a\n" || len(seen) != 1 {
		t.Fatalf("err = %v, stdout = %q, after saw %v", err, stdout, seen)
	}
	_, _, err = runBound(t, binding[fakeIn, fakeOut]{
		op:    fakeOp(true),
		input: itemsInput,
		after: func(*cobra.Command, fakeOut) error { t.Fatal("after ran on a partial result"); return nil },
	}, "a", "b")
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want partial", err)
	}
}
