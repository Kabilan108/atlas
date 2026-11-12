package snippet_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/snippet"
	"github.com/kabilan108/atlas/internal/ops/snippet/snippettest"
)

func newEnv(t *testing.T) (*opstest.Env, *snippettest.Fixture) {
	t.Helper()
	fixture := snippettest.New()
	return opstest.New(snippettest.Workspace).WithBitbucket(t, fixture), fixture
}

func requests(fixture *snippettest.Fixture) []string {
	var out []string
	for _, req := range fixture.Requests() {
		out = append(out, req.String())
	}
	return out
}

func TestListFiltersVisibilityBeforeLimit(t *testing.T) {
	env, fixture := newEnv(t)
	result, err := snippet.List.Call(context.Background(), env, snippet.ListInput{Visibility: "private", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snippets) != 1 || result.Snippets[0].ID != "abc123" {
		t.Fatalf("result = %+v, want only abc123", result)
	}
	if got := requests(fixture); len(got) != 1 || got[0] != "GET /2.0/snippets/acme?pagelen=100" {
		t.Fatalf("requests = %v, want one full page", got)
	}
	text, err := snippet.List.Text(result)
	if err != nil {
		t.Fatal(err)
	}
	want := "| ID | Title | Files | Visibility | Updated |\n| --- | --- | --- | --- | --- |\n| abc123 | Deploy notes | 2 | private | 2024-03-04 |\n" +
		"\nShowing the first 1; raise limit (max 100) for more.\n"
	if text != want {
		t.Fatalf("markdown = %q, want %q", text, want)
	}
}

func TestListRejectsUnknownRole(t *testing.T) {
	env, _ := newEnv(t)
	_, err := snippet.List.Invoke(context.Background(), env, json.RawMessage(`{"role":"admin"}`))
	if ops.KindOf(err) != ops.Invalid {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestViewMarkdownFencesEachFile(t *testing.T) {
	env, _ := newEnv(t)
	res, err := snippet.View.Invoke(context.Background(), env, json.RawMessage(`{"snippet":"https://bitbucket.org/snippets/acme/abc123"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Deploy notes\n\nSnippet acme/abc123, private, updated 2024-03-04, https://bitbucket.org/snippets/acme/abc123\n" +
		"\n## notes.md\n\n```markdown\n# Deploy\n\nRun the script.\n```\n" +
		"\n## run.sh\n\n```sh\n#!/bin/sh\necho deploy\n```\n"
	if res.Text != want {
		t.Fatalf("markdown = %q\nwant %q", res.Text, want)
	}
}

func TestViewMissingFileIsNotFound(t *testing.T) {
	env, _ := newEnv(t)
	_, err := snippet.View.Call(context.Background(), env, snippet.ViewInput{Target: snippet.Target{Snippet: "abc123"}, Filename: "nope"})
	if ops.KindOf(err) != ops.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestWritesSendTheExpectedForms(t *testing.T) {
	tests := []struct {
		name string
		def  ops.Def
		args string
		text string
		req  string
	}{
		{"create", snippet.Create, `{"title":"T","files":{"a.txt":"alpha"},"public":true}`,
			"Created snippet: new001\nURL: https://bitbucket.org/snippets/acme/new001\n",
			`POST /2.0/snippets/acme is_private=["false"] title=["T"] file[a.txt]="alpha"`},
		{"edit", snippet.Edit, `{"snippet":"abc123","title":"New","files":{"x.md":"x"},"remove":["run.sh"]}`,
			"Updated snippet: abc123\n",
			`PUT /2.0/snippets/acme/abc123 files=["run.sh"] title=["New"] file[x.md]="x"`},
		{"rename", snippet.Rename, `{"snippet":"abc123","from":"run.sh","to":"deploy.sh"}`,
			"Renamed run.sh to deploy.sh in snippet abc123\n",
			`PUT /2.0/snippets/acme/abc123 files=["run.sh"] file[deploy.sh]="#!/bin/sh\necho deploy"`},
		{"delete", snippet.Delete, `{"snippet":"abc123","workspace":"acme"}`,
			"Deleted snippet: abc123\n",
			`DELETE /2.0/snippets/acme/abc123`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newEnv(t)
			res, err := tc.def.Invoke(context.Background(), env, json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tc.text {
				t.Fatalf("text = %q, want %q", res.Text, tc.text)
			}
			got := requests(fixture)
			if last := got[len(got)-1]; last != tc.req {
				t.Fatalf("last request = %s, want %s", last, tc.req)
			}
		})
	}
}

func TestWritesRejectBadInput(t *testing.T) {
	tests := []struct {
		name string
		def  ops.Def
		args string
	}{
		{"create without files", snippet.Create, `{"title":"T","files":{}}`},
		{"create dot file name", snippet.Create, `{"title":"T","files":{"../x":"x"}}`},
		{"edit nothing", snippet.Edit, `{"snippet":"abc123"}`},
		{"rename absolute", snippet.Rename, `{"snippet":"abc123","from":"/etc/passwd","to":"x"}`},
		{"unsupported url", snippet.Delete, `{"snippet":"https://example.com/snippets/acme/abc123"}`},
		{"bad workspace", snippet.Delete, `{"snippet":"abc123","workspace":"a/b"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newEnv(t)
			_, err := tc.def.Invoke(context.Background(), env, json.RawMessage(tc.args))
			if ops.KindOf(err) != ops.Invalid {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if n := len(fixture.Requests()); n != 0 {
				t.Fatalf("made %d requests", n)
			}
		})
	}
}

func TestParseRefNeedsWorkspaceForID(t *testing.T) {
	if _, err := snippet.ParseRef("abc123", ""); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("err = %v, want workspace error", err)
	}
	ref, err := snippet.ParseRef("https://api.bitbucket.org/2.0/snippets/team/abc123", "")
	if err != nil || ref != (snippet.Ref{Workspace: "team", ID: "abc123"}) {
		t.Fatalf("ParseRef(api url) = %+v, %v", ref, err)
	}
}
