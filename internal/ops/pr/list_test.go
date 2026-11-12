package pr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestListOneRepo(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.List, env, `{"repo":"acme/widgets"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "2 open pull requests in acme/widgets\n\n" +
		"| PR | Title | Author | Branches | Updated | Reviews | Feedback |\n|---|---|---|---|---|---|---|\n" +
		"| #42 | Fix login: handle 'empty' users | Ada Lovelace | feature/login → main | 2025-03-05 16:30 UTC | 1 approved, 1 changes requested, 1 pending | 5 comments, 2 tasks |\n" +
		"| #43 | [draft] Tidy the docs | Grace Hopper | docs/tidy → main | 2025-03-04 10:00 UTC | 1 pending |  |\n"
	if result.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", result.Text, want)
	}
	wantCalls(t, fixture, "GET /2.0/repositories/acme/widgets/pullrequests?state=OPEN")
	data, err := json.Marshal(result.Value)
	if err != nil || !strings.HasPrefix(string(data), `[{"id":42,`) {
		t.Fatalf("JSON = %.40s (%v), want the bare array", data, err)
	}
}

func TestListFiltersAndEmpty(t *testing.T) {
	env, fixture := newTestEnv(t, "acme")
	result, err := invoke(t, pr.List, env, `{"repo":"widgets","author":"557058:grace"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Value.(*pr.ListResult).PullRequests; len(got) != 1 || got[0].ID != 43 {
		t.Fatalf("author filter kept %+v", got)
	}
	result, err = invoke(t, pr.List, env, `{"repo":"widgets","state":"merged"}`)
	if err != nil || result.Text != "No merged pull requests in acme/widgets.\n" {
		t.Fatalf("text = %q, err = %v", result.Text, err)
	}
	if got := calls(fixture); got[len(got)-1] != "GET /2.0/repositories/acme/widgets/pullrequests?state=MERGED" {
		t.Fatalf("requests = %v", got)
	}
}

func TestListWorkspace(t *testing.T) {
	for _, repo := range []string{"acme/*", "*"} {
		env, _ := newTestEnv(t, "acme")
		result, err := pr.List.Call(context.Background(), env, pr.ListInput{RepoRef: ops.RepoRef{Repo: repo}})
		if err != nil {
			t.Fatal(err)
		}
		text, err := result.Markdown()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(text, "3 open pull requests in acme/*") || !strings.Contains(text, "| acme/gadgets#7 | Gadget fix |") {
			t.Fatalf("%s:\n%s", repo, text)
		}
	}
}

func TestListWorkspacePartial(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	fixture.Fail(http.MethodGet, "/repositories/acme/gadgets/pullrequests", 0)
	result, err := invoke(t, pr.List, env, `{"repo":"acme/*"}`)
	if ops.KindOf(err) != ops.Partial || !strings.Contains(err.Error(), "1 of 2 repositories") {
		t.Fatalf("err = %v, want partial naming the failed repository", err)
	}
	if !strings.HasPrefix(result.Text, "2 open pull requests") {
		t.Fatalf("partial text = %q", result.Text)
	}
}

func TestListRejectsBadState(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	_, err := invoke(t, pr.List, env, `{"repo":"acme/widgets","state":"closed"}`)
	wantInvalid(t, err, fixture, "state")
}
