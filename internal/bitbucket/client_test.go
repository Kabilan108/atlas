package bitbucket

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/httpclient"
	"github.com/kabilan108/atlas/internal/parse"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestListRepositoriesPagination(t *testing.T) {
	t.Setenv(baseURLEnv, "https://api.test/2.0")

	call := 0
	handler := func(r *http.Request) (*http.Response, error) {
		call++
		switch call {
		case 1:
			expected := "https://api.test/2.0/repositories/workspace?pagelen=50"
			if r.URL.String() != expected {
				t.Fatalf("unexpected URL: %s", r.URL.String())
			}
			body := `{"values":[{"slug":"repo-a","name":"Repo A","full_name":"workspace/repo-a","links":{"html":{"href":"https://bitbucket.org/workspace/repo-a"}}}],"next":"https://api.test/2.0/repositories/workspace?page=2"}`
			return jsonResponse(http.StatusOK, body, r), nil
		case 2:
			expected := "https://api.test/2.0/repositories/workspace?page=2"
			if r.URL.String() != expected {
				t.Fatalf("unexpected URL on page 2: %s", r.URL.String())
			}
			body := `{"values":[{"slug":"repo-b","name":"Repo B","full_name":"workspace/repo-b","links":{"html":{"href":"https://bitbucket.org/workspace/repo-b"}}}]}`
			return jsonResponse(http.StatusOK, body, r), nil
		default:
			t.Fatalf("unexpected number of calls: %d", call)
		}
		return nil, nil
	}

	hc, err := httpclient.New(httpclient.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}), httpclient.WithCredentials("user", "token"))
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	client, err := NewClient(hc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	repos, err := client.ListRepositories(context.Background(), "workspace", 0)
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	if repos[0].Slug != "repo-a" || repos[1].Slug != "repo-b" {
		t.Fatalf("unexpected repositories: %+v", repos)
	}
}

func TestGetPullRequestWithDiff(t *testing.T) {
	t.Setenv(baseURLEnv, "https://api.test/2.0")

	call := 0
	handler := func(r *http.Request) (*http.Response, error) {
		call++
		switch call {
		case 1:
			expected := "https://api.test/2.0/repositories/workspace/repo/pullrequests/42"
			if r.URL.String() != expected {
				t.Fatalf("unexpected PR URL: %s", r.URL.String())
			}
			body := `{
                "id":42,
                "title":"Add feature",
                "summary":{"raw":"Feature details"},
                "state":"OPEN",
                "author":{"display_name":"Alex"},
                "updated_on":"2024-02-02T10:00:00.000000+00:00",
                "source":{"branch":{"name":"feature"}},
                "destination":{"branch":{"name":"main"}},
                "links":{"html":{"href":"https://bitbucket.org/workspace/repo/pull-requests/42"}}
            }`
			return jsonResponse(http.StatusOK, body, r), nil
		case 2:
			expected := "https://api.test/2.0/repositories/workspace/repo/pullrequests/42/diff"
			if r.URL.String() != expected {
				t.Fatalf("unexpected diff URL: %s", r.URL.String())
			}
			return textResponse(http.StatusOK, "diff --git a/file b/file", r), nil
		default:
			t.Fatalf("unexpected number of calls: %d", call)
		}
		return nil, nil
	}

	hc, err := httpclient.New(httpclient.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}), httpclient.WithCredentials("user", "token"))
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	client, err := NewClient(hc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ref := parse.PullRequestRef{Workspace: "workspace", RepoSlug: "repo", ID: 42}
	pr, err := client.GetPullRequest(context.Background(), ref, true)
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}

	if pr.Title != "Add feature" || pr.SourceBranch != "feature" || pr.DestinationBranch != "main" {
		t.Fatalf("unexpected pr data: %+v", pr)
	}

	if pr.Diff != "diff --git a/file b/file" {
		t.Fatalf("unexpected diff: %q", pr.Diff)
	}

	expectedTime := time.Date(2024, 2, 2, 10, 0, 0, 0, time.UTC)
	if !pr.Updated.Equal(expectedTime) {
		t.Fatalf("unexpected updated time: %s", pr.Updated)
	}
}

func TestSearchPullRequests(t *testing.T) {
	t.Setenv(baseURLEnv, "https://api.test/2.0")

	call := 0
	handler := func(r *http.Request) (*http.Response, error) {
		call++
		switch call {
		case 1:
			expected := "https://api.test/2.0/repositories/workspace?pagelen=50"
			if r.URL.String() != expected {
				t.Fatalf("unexpected repo list URL: %s", r.URL.String())
			}
			body := `{"values":[{"slug":"repo"}]}`
			return jsonResponse(http.StatusOK, body, r), nil
		case 2:
			expectedPrefix := "https://api.test/2.0/repositories/workspace/repo/pullrequests"
			if !strings.HasPrefix(r.URL.String(), expectedPrefix) {
				t.Fatalf("unexpected pull request URL: %s", r.URL.String())
			}
			q := r.URL.Query().Get("q")
			expectedQ := `(title ~ "bug" OR summary.raw ~ "bug") AND state = "OPEN"`
			if q != expectedQ {
				t.Fatalf("unexpected query: %s", q)
			}
			body := `{"values":[{"id":1,"title":"Bug fix","summary":{"raw":"Fix bug"},"state":"OPEN","author":{"display_name":"Alex"},"updated_on":"2024-01-01T00:00:00Z","links":{"html":{"href":"https://bitbucket.org/workspace/repo/pull-requests/1"}}}]}`
			return jsonResponse(http.StatusOK, body, r), nil
		default:
			t.Fatalf("unexpected number of calls: %d", call)
		}
		return nil, nil
	}

	hc, err := httpclient.New(httpclient.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}), httpclient.WithCredentials("user", "token"))
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	client, err := NewClient(hc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	opts := SearchPROptions{Query: "bug", Limit: 1}
	results, err := client.SearchPullRequests(context.Background(), "workspace", opts)
	if err != nil {
		t.Fatalf("search prs: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].ID != 1 || results[0].RepoSlug != "repo" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func jsonResponse(status int, body string, req *http.Request) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func textResponse(status int, body string, req *http.Request) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "text/plain")
	return resp
}
