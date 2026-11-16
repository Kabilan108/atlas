package repo_test

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/repo"
	"github.com/kabilan108/atlas/internal/ops/repo/repotest"
)

const featureSpec = repotest.FeatureHead + "..main?topic=true"

func TestDiffRequests(t *testing.T) {
	branchLookup := "GET /2.0/repositories/acme/widgets/refs/branches/feature/login"
	tests := []struct {
		name string
		args string
		want []string
	}{
		{
			name: "head goes first in the spec, with three-dot topic diffs",
			args: `{"repo":"widgets","base":"main","head":"feature/login"}`,
			want: []string{
				branchLookup,
				"GET /2.0/repositories/acme/widgets/diffstat/" + featureSpec,
				"GET /2.0/repositories/acme/widgets/diff/" + featureSpec,
			},
		},
		{
			name: "files_only skips the diff",
			args: `{"repo":"widgets","base":"main","head":"feature/login","files_only":true}`,
			want: []string{branchLookup, "GET /2.0/repositories/acme/widgets/diffstat/" + featureSpec},
		},
		{
			name: "no changed files skips the diff",
			args: `{"repo":"widgets","base":" main ","head":"main"}`,
			want: []string{"GET /2.0/repositories/acme/widgets/diffstat/main..main?topic=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, fixture := newEnv(t)
			if _, err := repo.Diff.Invoke(context.Background(), env, json.RawMessage(tt.args)); err != nil {
				t.Fatal(err)
			}
			if got := fixture.Requests(); !slices.Equal(got, tt.want) {
				t.Fatalf("requests =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestDiffMarkdown(t *testing.T) {
	summary := "Changes in acme/widgets main...feature/login: 3 files, +3 -0\n\n" +
		"| Status | File | Added | Removed |\n" +
		"| --- | --- | --- | --- |\n" +
		"| modified | src/login.go | +2 | -0 |\n" +
		"| renamed | old.go → new.go | +0 | -0 |\n" +
		"| added | docs/login.md | +1 | -0 |\n"
	feature := `"repo":"widgets","base":"main","head":"feature/login"`
	tests := []struct {
		name string
		args string
		want string
	}{
		{"summary then diff", `{` + feature + `}`, summary + "\n" + repotest.FeatureDiff},
		{"files only", `{` + feature + `,"files_only":true}`, summary},
		{"path keeps one file's section", `{` + feature + `,"path":"docs/"}`, summary + "\n" + repotest.FeatureDiff[strings.Index(repotest.FeatureDiff, "diff --git a/docs"):]},
		{"path that matches nothing", `{` + feature + `,"path":"doc"}`, summary + "\nNo changes under doc.\n"},
		{"no changes", `{"repo":"widgets","base":"main","head":"main"}`, "No changes in acme/widgets main...main: main adds nothing since its merge base with main.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := invoke(t, repo.Diff, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tt.want {
				t.Fatalf("markdown =\n%s\nwant\n%s", res.Text, tt.want)
			}
		})
	}
}

func TestDiffResultJSON(t *testing.T) {
	res, err := invoke(t, repo.Diff, `{"repo":"widgets","base":"main","head":"feature/login","files_only":true}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(res.Value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"repo":"acme/widgets","base":"main","head":"feature/login","files":[` +
		`{"status":"modified","path":"src/login.go","lines_added":2,"lines_removed":0},` +
		`{"status":"renamed","path":"new.go","old_path":"old.go","lines_added":0,"lines_removed":0},` +
		`{"status":"added","path":"docs/login.md","lines_added":1,"lines_removed":0}],"diff":""}`
	if string(data) != want {
		t.Fatalf("json =\n%s\nwant\n%s", data, want)
	}
}

func TestDiffTruncation(t *testing.T) {
	res, err := invoke(t, repo.Diff, `{"repo":"widgets","base":"main","head":"feature/login","max_bytes":100}`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Value.(*repo.DiffResult)
	if !out.Truncated || len(out.Diff) > 100 || !strings.HasSuffix(out.Diff, "\n") || !strings.HasPrefix(repotest.FeatureDiff, out.Diff) {
		t.Fatalf("diff = %q, truncated = %v", out.Diff, out.Truncated)
	}
	note := "\n[diff truncated: showing " + strconv.Itoa(len(out.Diff)) + " of " + strconv.Itoa(len(repotest.FeatureDiff)) +
		" bytes; pass path for one file or raise max_bytes.]\n"
	if !strings.HasSuffix(res.Text, out.Diff+note) {
		t.Fatalf("markdown = %q, want it to end with the diff and %q", res.Text, note)
	}
}

func TestDiffListsAtMostMaxFiles(t *testing.T) {
	res, err := invoke(t, repo.Diff, `{"repo":"widgets","base":"main","head":"`+repotest.ManyFilesHead+`","files_only":true}`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Value.(*repo.DiffResult)
	if len(out.Files) != 500 || !out.MoreFiles {
		t.Fatalf("files = %d, more = %v; want the first 500 and more", len(out.Files), out.MoreFiles)
	}
	if !strings.HasPrefix(res.Text, "Changes in acme/widgets main...many: 500 files (the first 500), +500 -0\n") {
		t.Fatalf("markdown starts %q", res.Text[:120])
	}
}

func TestDiffLineNumbers(t *testing.T) {
	res, err := invoke(t, repo.Diff, `{"repo":"widgets","base":"main","head":"feature/login","path":"src/login.go","line_numbers":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "+++ b/src/login.go\n@@ -8,3 +8,5 @@ func Login(user string) error {\n 8  8  \tif user == \"\" {\n    9 +\t\tlog(user)\n") {
		t.Fatalf("markdown = %q, want numbered lines", res.Text)
	}
}

func TestDiffErrors(t *testing.T) {
	tests := []struct {
		name string
		args string
		kind ops.Kind
		text string
	}{
		{"missing base", `{"repo":"widgets","head":"feature/login"}`, ops.Invalid, "base is required"},
		{"blank head", `{"repo":"widgets","base":"main","head":"  "}`, ops.Invalid, "head is required"},
		{"range in one ref", `{"repo":"widgets","base":"main..feature/login","head":"x"}`, ops.Invalid, `cannot contain ".."`},
		{"path with files_only", `{"repo":"widgets","base":"main","head":"x","path":"src","files_only":true}`, ops.Invalid, "files_only"},
		{"negative max_bytes", `{"repo":"widgets","base":"main","head":"x","max_bytes":-1}`, ops.Invalid, "max_bytes"},
		{"max_bytes over the maximum", `{"repo":"widgets","base":"main","head":"x","max_bytes":` + strconv.Itoa(pr.MaxDiffBytes+1) + `}`, ops.Invalid, "max_bytes"},
		{"unknown ref", `{"repo":"widgets","base":"main","head":"nope"}`, ops.NotFound, ""},
		{"unknown slash ref", `{"repo":"widgets","base":"main","head":"feature/nope"}`, ops.NotFound, "feature/nope"},
		{"server error", `{"repo":"broken","base":"main","head":"x"}`, ops.Upstream, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := invoke(t, repo.Diff, tt.args)
			if ops.KindOf(err) != tt.kind || !strings.Contains(err.Error(), tt.text) {
				t.Fatalf("err = %v (%v), want %v containing %q", err, ops.KindOf(err), tt.kind, tt.text)
			}
		})
	}
}
