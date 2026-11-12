package pr_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
	"github.com/kabilan108/atlas/internal/output"
)

type testEnv struct {
	client    *bitbucket.Client
	workspace string
}

func (e testEnv) Bitbucket(context.Context) (*bitbucket.Client, error) { return e.client, nil }
func (e testEnv) DefaultWorkspace() string                             { return e.workspace }

func newTestEnv(t *testing.T, workspace string) (testEnv, *prtest.Fixture) {
	t.Helper()
	fixture := prtest.New()
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	client, err := bitbucket.NewClientWithCredentials("user", "token",
		bitbucket.WithBaseURL(server.URL+"/2.0"))
	if err != nil {
		t.Fatal(err)
	}
	return testEnv{client: client, workspace: workspace}, fixture
}

func TestViewMarkdownMatchesGolden(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		in        pr.ViewInput
		golden    string
	}{
		{"number", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}}, "view.md"},
		{"hash number", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "#42"}}, "view.md"},
		{"bare repo uses default workspace", "acme", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "widgets"}, PR: "42"}}, "view.md"},
		{"comments", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}, Comments: true}, "view_comments.md"},
		{"resolved", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}, Comments: true, IncludeResolved: true}, "view_comments_all.md"},
		{"branch", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: prtest.Branch}, Comments: true}, "view_comments.md"},
		{"current branch", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, CurrentBranch: prtest.Branch}, Comments: true}, "view_comments.md"},
		{"url without repo", "", pr.ViewInput{Ref: pr.Ref{PR: prtest.URL}, Comments: true}, "view_comments.md"},
		{"url wins over repo", "", pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "other/elsewhere"}, PR: prtest.URL}, Comments: true}, "view_comments.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newTestEnv(t, tc.workspace)
			result, err := pr.View.Call(context.Background(), env, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			served := len(fixture.Requests())
			got, err := result.Markdown()
			if err != nil {
				t.Fatal(err)
			}
			if want := prtest.Golden(tc.golden); got != want {
				t.Fatalf("markdown differs from %s\n--- got ---\n%s", tc.golden, got)
			}
			if after := len(fixture.Requests()); after != served {
				t.Fatalf("Markdown made %d Bitbucket requests", after-served)
			}
			for _, req := range fixture.Requests() {
				if strings.HasPrefix(req.Path, "/2.0/repositories/") && !strings.HasPrefix(req.Path, "/2.0/repositories/acme/widgets/") {
					t.Fatalf("request outside the PR's repository: %s", req.Path)
				}
			}
		})
	}
}

func TestViewJSONMatchesGolden(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := pr.View.Call(context.Background(), env, pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}, JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	if want := prtest.Golden("view.json"); buf.String() != want {
		t.Fatalf("JSON differs from view.json\n--- got ---\n%s", buf.String())
	}
	for _, req := range fixture.Requests() {
		if strings.Contains(req.Path, "/tasks") || strings.Contains(req.Path, "/diff") || strings.Contains(req.Path, "/users/") {
			t.Fatalf("JSON view fetched %s, which its output does not use", req.Path)
		}
	}
}

func TestViewInvokeDecodesArguments(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := pr.View.Invoke(context.Background(), env, json.RawMessage(`{"repo":"acme/widgets","pr":"42","comments":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := prtest.Golden("view_comments.md"); result.Text != want {
		t.Fatalf("Invoke text differs from view_comments.md\n--- got ---\n%s", result.Text)
	}
	if req := fixture.Requests()[0]; req.Username != "user" || req.Password != "token" {
		t.Fatalf("request credentials = %q/%q", req.Username, req.Password)
	}
}

func TestViewRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"cli-only field", `{"repo":"acme/widgets","pr":"42","CurrentBranch":"main"}`, "unknown field"},
		{"unknown field", `{"repo":"acme/widgets","pr":"42","bogus":true}`, "unknown field"},
		{"wrong type", `{"repo":"acme/widgets","pr":42}`, "invalid arguments"},
		{"missing pr", `{"repo":"acme/widgets"}`, "pr is required"},
		{"missing repo", `{"pr":"42"}`, "repo is required"},
		{"bare repo without default workspace", `{"repo":"widgets","pr":"42"}`, "no default workspace"},
		{"bad repo", `{"repo":"a/b/c","pr":"42"}`, "invalid repository"},
		{"bad url", `{"pr":"https://bitbucket.org/acme/widgets/pull-requests/x"}`, "invalid pull request URL"},
		{"zero", `{"repo":"acme/widgets","pr":"0"}`, "invalid pull request number"},
		// The confirmed P2: this once requested /repositories/acme/widgets?fields=description#/...
		{"query in repo", `{"repo":"acme/widgets?fields=description#","pr":"42"}`, "must not contain"},
		{"fragment in workspace", `{"repo":"ac#me/widgets","pr":"42"}`, "must not contain"},
		{"dot repo", `{"repo":"acme/..","pr":"42"}`, "dot segments"},
		{"url with escaped slash", `{"pr":"https://bitbucket.org/acme/wid%2Fgets/pull-requests/42"}`, "must not contain"},
		{"url with escaped query", `{"pr":"https://bitbucket.org/acme/wid%3Fx=1/pull-requests/42"}`, "must not contain"},
		{"url with dot segment", `{"pr":"https://bitbucket.org/acme/%2E%2E/pull-requests/42"}`, "dot segments"},
		{"trailing data", `{"repo":"acme/widgets","pr":"42"} {}`, "trailing data"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			_, err := pr.View.Invoke(context.Background(), env, json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
			if kind := ops.KindOf(err); kind != ops.Invalid {
				t.Fatalf("kind = %v, want invalid", kind)
			}
			if n := len(fixture.Requests()); n != 0 {
				t.Fatalf("invalid input made %d requests", n)
			}
		})
	}
}

func TestViewMissingPRIsNotFound(t *testing.T) {
	env, _ := newTestEnv(t, "")
	_, err := pr.View.Call(context.Background(), env, pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "7"}})
	if kind := ops.KindOf(err); kind != ops.NotFound {
		t.Fatalf("KindOf(%v) = %v, want not_found", err, kind)
	}
}

func TestParseSelector(t *testing.T) {
	tests := []struct {
		raw  string
		want pr.Selector
	}{
		{"", pr.Selector{}},
		{"  ", pr.Selector{}},
		{"12", pr.Selector{ID: 12}},
		{"#12", pr.Selector{ID: 12}},
		{"feature/x", pr.Selector{Branch: "feature/x"}},
		{"https://bitbucket.org/ws/repo/pull-requests/9", pr.Selector{Workspace: "ws", Repo: "repo", ID: 9}},
		{"https://bitbucket.org/ws/repo/pull-requests/9/overview", pr.Selector{Workspace: "ws", Repo: "repo", ID: 9}},
		{"https://example.com/ws/repo/pull-requests/9", pr.Selector{Branch: "https://example.com/ws/repo/pull-requests/9"}},
	}
	for _, tc := range tests {
		got, err := pr.ParseSelector(tc.raw)
		if err != nil {
			t.Fatalf("ParseSelector(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseSelector(%q) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}
