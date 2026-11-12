package pr_test

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
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
	for _, limit := range []int{-1, pr.MaxDiffBytes + 1} {
		_, err := pr.Diff.Call(t.Context(), env, pr.DiffInput{Ref: ref42(), MaxBytes: limit})
		if ops.KindOf(err) != ops.Invalid {
			t.Fatalf("max_bytes %d: err = %v, want invalid", limit, err)
		}
	}
	whole, err := pr.Diff.Call(t.Context(), env, pr.DiffInput{Ref: ref42(), MaxBytes: pr.MaxDiffBytes})
	if err != nil || whole.Truncated {
		t.Fatalf("max_bytes at the maximum: truncated=%v err=%v", whole.Truncated, err)
	}
}

func TestDiffLineNumbers(t *testing.T) {
	env, _ := newTestEnv(t, "")
	result, err := invoke(t, pr.Diff, env, `{"repo":"acme/widgets","pr":"42","line_numbers":true}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "diff --git a/src/login.go b/src/login.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/src/login.go\n" +
		"+++ b/src/login.go\n" +
		"@@ -8,5 +8,7 @@ func Login(user string) error {\n" +
		" 8  8  \tif user == \"\" {\n" +
		" 9  9  \t\treturn errEmpty\n" +
		"10 10  \t}\n" +
		"   11 +\ttoken, err := issue(user)\n" +
		"   12 +\tif err != nil { return err }\n" +
		"11 13  \treturn save(token)\n" +
		"12 14  }\n"
	if result.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", result.Text, want)
	}
}
