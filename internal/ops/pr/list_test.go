package pr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

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
		"| PR | Title | Author | Branches | Created | Updated | Reviews | Feedback | URL |\n|---|---|---|---|---|---|---|---|---|\n" +
		"| #42 | Fix login: handle 'empty' users | Ada Lovelace | feature/login → main | 2025-03-01 09:00 UTC | 2025-03-05 16:30 UTC | 1 approved, 1 changes requested, 1 pending | 5 comments, 2 tasks | https://bitbucket.org/acme/widgets/pull-requests/42 |\n" +
		"| #43 | [draft] Tidy the docs | Grace Hopper | docs/tidy → main | 2025-03-04 09:00 UTC | 2025-03-04 10:00 UTC | 1 pending |  | https://bitbucket.org/acme/widgets/pull-requests/43 |\n"
	if result.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", result.Text, want)
	}
	wantCalls(t, fixture, "GET /2.0/repositories/acme/widgets/pullrequests?pagelen=50&sort=-updated_on&state=OPEN")
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
	if got := calls(fixture); got[len(got)-1] != "GET /2.0/repositories/acme/widgets/pullrequests?pagelen=50&sort=-updated_on&state=MERGED" {
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

func TestListSeveralReposNewestFirst(t *testing.T) {
	env, fixture := newTestEnv(t, "acme")
	result, err := invoke(t, pr.List, env, `{"repos":["gadgets","acme/widgets","widgets"],"limit":5}`)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fixture,
		"GET /2.0/repositories/acme/gadgets/pullrequests?pagelen=5&sort=-updated_on&state=OPEN",
		"GET /2.0/repositories/acme/widgets/pullrequests?pagelen=5&sort=-updated_on&state=OPEN")
	var ids []string
	for _, p := range result.Value.(*pr.ListResult).PullRequests {
		ids = append(ids, p.Destination.Repository.FullName+"#"+itoa(p.ID))
	}
	if strings.Join(ids, " ") != "acme/widgets#42 acme/widgets#43 acme/gadgets#7" {
		t.Fatalf("order = %v, want newest updated first across repos", ids)
	}
	if !strings.HasPrefix(result.Text, "3 open pull requests in 2 repos\n") || !strings.Contains(result.Text, "| acme/gadgets#7 |") {
		t.Fatalf("text:\n%s", result.Text)
	}
}

func TestListSeveralReposPartial(t *testing.T) {
	env, fixture := newTestEnv(t, "acme")
	fixture.Fail(http.MethodGet, "/repositories/acme/gadgets/pullrequests", 0)
	result, err := invoke(t, pr.List, env, `{"repos":["widgets","gadgets"]}`)
	if ops.KindOf(err) != ops.Partial || !strings.Contains(err.Error(), "acme/gadgets: ") {
		t.Fatalf("err = %v, want partial naming acme/gadgets", err)
	}
	if !strings.HasPrefix(result.Text, "2 open pull requests in 2 repos") {
		t.Fatalf("partial text = %q", result.Text)
	}
}

func TestListUpdatedSinceAllStatesAndMore(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.List, env, `{"repo":"acme/widgets","state":"all","updated_since":"2025-03-05","limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fixture, "GET /2.0/repositories/acme/widgets/pullrequests?pagelen=1&q=updated_on+%3E%3D+2025-03-05T00%3A00%3A00%2B00%3A00&sort=-updated_on&state=OPEN&state=MERGED&state=DECLINED&state=SUPERSEDED")
	for _, want := range []string{
		"1 pull request (all states) in acme/widgets updated since 2025-03-05 00:00 UTC\n",
		"| PR | Title | Author | State | Branches |",
		"| #42 | Fix login: handle 'empty' users | Ada Lovelace | OPEN |",
	} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("text lacks %q:\n%s", want, result.Text)
		}
	}
	if strings.Contains(result.Text, "#43") {
		t.Fatalf("#43 was updated before updated_since:\n%s", result.Text)
	}
}

func TestListMoreNote(t *testing.T) {
	env, _ := newTestEnv(t, "")
	result, err := invoke(t, pr.List, env, `{"repo":"acme/widgets","limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result.Text, "\nShowing the newest 1 per repo; acme/widgets has more. Raise limit (max 500) or set updated_since.\n") {
		t.Fatalf("text:\n%s", result.Text)
	}
}

func TestListRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ args, mention string }{
		{`{"repo":"acme/widgets","limit":501}`, "limit"},
		{`{"repo":"acme/widgets","updated_since":"last week"}`, "updated_since"},
		{`{"repo":"acme/widgets","repos":["acme/gadgets"]}`, "not both"},
		{`{"repos":["a/b/c"]}`, "repos[0]"},
	} {
		env, fixture := newTestEnv(t, "acme")
		_, err := invoke(t, pr.List, env, tc.args)
		wantInvalid(t, err, fixture, tc.mention)
	}
	env, fixture := newTestEnv(t, "acme")
	repos := make([]string, pr.MaxListRepos+1)
	for i := range repos {
		repos[i] = "r" + itoa(i)
	}
	_, err := pr.List.Call(context.Background(), env, pr.ListInput{Repos: repos})
	wantInvalid(t, err, fixture, "limit is 50")
}

func TestParseUpdatedSince(t *testing.T) {
	for raw, want := range map[string]string{
		"2025-03-05":                   "2025-03-05T00:00:00Z",
		"2025-03-05T09:30:00Z":         "2025-03-05T09:30:00Z",
		"2025-03-05T09:30:00-05:00":    "2025-03-05T14:30:00Z",
		"2025-03-05T09:30":             "2025-03-05T09:30:00Z",
		"2025-03-05T09:30:00.000-0400": "2025-03-05T13:30:00Z",
	} {
		got, err := pr.ParseUpdatedSince(raw)
		if err != nil || got.UTC().Format(time.RFC3339) != want {
			t.Errorf("ParseUpdatedSince(%q) = %v, %v; want %s", raw, got, err, want)
		}
	}
}
