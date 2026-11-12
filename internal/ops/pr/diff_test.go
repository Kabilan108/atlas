package pr_test

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestDiff(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	full, err := invoke(t, pr.Diff, env, `{"repo":"acme/widgets","pr":"42"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(full.Text, "diff --git a/src/login.go b/src/login.go\n") || !strings.HasSuffix(full.Text, " }\n") {
		t.Fatalf("text = %q", full.Text)
	}
	wantCalls(t, fixture, "GET "+prPath, "GET "+prPath+"/diff")

	for _, path := range []string{"src", "src/", "src/login.go"} {
		filtered, err := invoke(t, pr.Diff, env, `{"repo":"acme/widgets","pr":"42","path":"`+path+`"}`)
		if err != nil || filtered.Text != full.Text {
			t.Fatalf("path %s: text = %q, err = %v", path, filtered.Text, err)
		}
	}
	none, err := invoke(t, pr.Diff, env, `{"repo":"acme/widgets","pr":"42","path":"sr"}`)
	if err != nil || none.Text != "No changes under sr. Changed files:\n- src/login.go\n" {
		t.Fatalf("unmatched path: text = %q, err = %v", none.Text, err)
	}
}

func TestDiffCap(t *testing.T) {
	env, _ := newTestEnv(t, "")
	result, err := invoke(t, pr.Diff, env, `{"repo":"acme/widgets","pr":"42","max_bytes":100}`)
	if err != nil {
		t.Fatal(err)
	}
	out := result.Value.(*pr.DiffResult)
	if !out.Truncated || len(out.Diff) > 100 || !strings.HasSuffix(out.Diff, "\n") {
		t.Fatalf("diff = %q, truncated = %v", out.Diff, out.Truncated)
	}
	if !strings.Contains(result.Text, "[diff truncated: showing "+itoa(len(out.Diff))+" of ") || !strings.HasSuffix(result.Text, "- src/login.go\n") {
		t.Fatalf("text = %q", result.Text)
	}
	unlimited, err := pr.Diff.Call(t.Context(), env, pr.DiffInput{Ref: ref42(), MaxBytes: -1})
	if err != nil || unlimited.Truncated {
		t.Fatalf("max_bytes -1 truncated: %v", err)
	}
}
