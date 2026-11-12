package bitbucket

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSearchRepositoriesSendsNameQueryAndPaginates(t *testing.T) {
	t.Parallel()

	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, `{"values":[{"name":"api-two"}]}`)
			return
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"next":"%s/repositories/ws?page=2","values":[{"name":"api-one","mainbranch":{"name":"main"}}]}`, serverURL))
	})
	serverURL = server.URL

	repos, err := client.SearchRepositories("ws", &RepositoryListOptions{Name: `api "core"`, Sort: "-updated_on"})
	if err != nil {
		t.Fatalf("SearchRepositories() error = %v", err)
	}
	if len(repos) != 2 || repos[0].MainBranch.Name != "main" || repos[1].Name != "api-two" {
		t.Fatalf("repos = %#v", repos)
	}

	req := recorder.all()[0]
	expectRequest(t, req, http.MethodGet, "/repositories/ws")
	if got := req.Query.Get("q"); got != `name ~ "api \"core\""` {
		t.Fatalf("q = %q", got)
	}
	if req.Query.Get("sort") != "-updated_on" || req.Query.Get("pagelen") != "100" {
		t.Fatalf("query = %v", req.Query)
	}
}

func TestSearchRepositoriesLimit(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"next":"https://elsewhere/ignored","values":[{"name":"a"},{"name":"b"},{"name":"c"}]}`)
	})

	repos, err := client.SearchRepositories("ws", &RepositoryListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("SearchRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	req := recorder.all()[0]
	if req.Query.Get("pagelen") != "2" || req.Query.Has("q") {
		t.Fatalf("query = %v", req.Query)
	}
}

func TestListAndGetBranches(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path == "/repositories/ws/repo/refs/branches" {
			writeJSON(w, http.StatusOK, `{"values":[{"name":"feature/login","target":{"hash":"abc","message":"wip","author":{"raw":"Jane <j@x>"}}}]}`)
			return
		}
		if r.URL.Path == "/repositories/ws/repo/refs/branches/feature/missing" {
			writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"Branch not found"}}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"name":"feature/login","target":{"hash":"abc","date":"2026-01-02T03:04:05Z"},"merge_strategies":["merge_commit","squash"],"default_merge_strategy":"squash"}`)
	})

	branches, err := client.ListBranches("ws", "repo", &BranchListOptions{Name: "login", Sort: "-target.date"})
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(branches) != 1 || branches[0].Target.Hash != "abc" || branches[0].Target.Author.Raw != "Jane <j@x>" {
		t.Fatalf("branches = %#v", branches)
	}

	branch, err := client.GetBranch("ws", "repo", "feature/login")
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if branch.DefaultMergeStrategy != MergeStrategySquash || len(branch.MergeStrategies) != 2 || branch.Target.Date.IsZero() {
		t.Fatalf("branch = %#v", branch)
	}

	_, err = client.GetBranch("ws", "repo", "feature/missing")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "branch 'feature/missing'") {
		t.Fatalf("missing branch error = %v", err)
	}

	requests := recorder.all()
	list := requests[0]
	if list.Query.Get("q") != `name ~ "login"` || list.Query.Get("sort") != "-target.date" || list.Query.Get("pagelen") != "100" {
		t.Fatalf("list query = %v", list.Query)
	}
	expectRequest(t, requests[1], http.MethodGet, "/repositories/ws/repo/refs/branches/feature/login")
	if requests[1].EscapedPath != "/repositories/ws/repo/refs/branches/feature/login" {
		t.Fatalf("escaped path = %s, want slash preserved", requests[1].EscapedPath)
	}
}

func TestListCommits(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"next":"https://not-followed","values":[{"hash":"c1","message":"one","date":"2026-01-02T03:04:05Z","author":{"raw":"Jane","user":{"display_name":"Jane"}},"parents":[{"hash":"c0"}]}]}`)
	})

	commits, err := client.ListCommits("ws", "repo", "main", &CommitListOptions{Exclude: []string{"release"}, Path: "go.mod"})
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if len(commits) != 1 || commits[0].Hash != "c1" || commits[0].Author.User.DisplayName != "Jane" || commits[0].Parents[0].Hash != "c0" {
		t.Fatalf("commits = %#v", commits)
	}

	if _, err := client.ListCommits("ws", "repo", "feature/x", &CommitListOptions{Include: []string{"hotfix"}, Exclude: []string{"main"}, Limit: 1}); err != nil {
		t.Fatalf("ListCommits(slash) error = %v", err)
	}

	requests := recorder.all()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2 (limit and default stop after one page)", len(requests))
	}
	expectRequest(t, requests[0], http.MethodGet, "/repositories/ws/repo/commits/main")
	if requests[0].Query.Get("exclude") != "release" || requests[0].Query.Get("path") != "go.mod" || requests[0].Query.Has("pagelen") {
		t.Fatalf("first query = %v", requests[0].Query)
	}
	expectRequest(t, requests[1], http.MethodGet, "/repositories/ws/repo/commits")
	want := url.Values{"include": {"feature/x", "hotfix"}, "exclude": {"main"}, "pagelen": {"1"}}
	if requests[1].Query.Encode() != want.Encode() {
		t.Fatalf("second query = %s, want %s", requests[1].Query.Encode(), want.Encode())
	}
}

func TestGetFileContent(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/repositories/ws/repo/src/main/config/version.json":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, `{"version":"1.2.3"}`)
		case "/repositories/ws/repo/refs/branches/release/1.2":
			writeJSON(w, http.StatusOK, `{"name":"release/1.2","target":{"hash":"feedface"}}`)
		case "/repositories/ws/repo/src/feedface/version.json":
			_, _ = io.WriteString(w, "from release")
		default:
			writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"No such file or directory"}}`)
		}
	})

	data, err := client.GetFileContent("ws", "repo", "main", "config/version.json")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v", err)
	}
	if string(data) != `{"version":"1.2.3"}` {
		t.Fatalf("content = %q", data)
	}
	if user, _, ok := (&http.Request{Header: recorder.all()[0].Header}).BasicAuth(); !ok || user != "user" {
		t.Fatal("file request missing credentials")
	}

	data, err = client.GetFileContent("ws", "repo", "release/1.2", "version.json")
	if err != nil {
		t.Fatalf("GetFileContent(slash ref) error = %v", err)
	}
	if string(data) != "from release" {
		t.Fatalf("slash ref content = %q", data)
	}

	_, err = client.GetFileContent("ws", "repo", "main", "missing.json")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "file 'main:missing.json'") {
		t.Fatalf("missing file error = %v", err)
	}

	if _, err := client.GetFileContent("ws", "repo", "main", "../etc/passwd"); err == nil {
		t.Fatal("dot segment error = nil, want error")
	}
}
