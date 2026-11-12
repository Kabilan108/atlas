package pr_test

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

const prPath = "/2.0/repositories/acme/widgets/pullrequests/42"

func ref42() pr.Ref {
	return pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}
}

// calls lists the fixture's requests as "METHOD path", with each write's
// body on the next entry.
func calls(fixture *prtest.Fixture) []string {
	var out []string
	for _, req := range fixture.Requests() {
		out = append(out, req.Method+" "+req.Path)
		if req.Body != "" {
			out = append(out, "  "+req.Body)
		}
	}
	return out
}

func wantCalls(t *testing.T, fixture *prtest.Fixture, want ...string) {
	t.Helper()
	if got := calls(fixture); !slices.Equal(got, want) {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func invoke(t *testing.T, def ops.Def, env ops.Env, args string) (ops.Result, error) {
	t.Helper()
	return def.Invoke(context.Background(), env, json.RawMessage(args))
}

func wantInvalid(t *testing.T, err error, fixture *prtest.Fixture, mention string) {
	t.Helper()
	if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), mention) {
		t.Fatalf("error = %v (%v), want invalid mentioning %q", err, ops.KindOf(err), mention)
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("invalid input made %d requests: %v", n, calls(fixture))
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
