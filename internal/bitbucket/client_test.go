package bitbucket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBitbucketClient_SearchRepositories(t *testing.T) {
	mockResponse := RepositorySearchResult{
		Values: []Repository{
			{
				Name:        "test-repo",
				FullName:    "workspace/test-repo",
				Description: "A test repository",
				IsPrivate:   false,
				Owner: User{
					DisplayName: "Test Owner",
					Username:    "testowner",
				},
				UpdatedOn: "2023-01-01T12:00:00.000000+00:00",
				Links: RepoLinks{
					HTML: struct {
						Href string `json:"href"`
					}{
						Href: "https://bitbucket.org/workspace/test-repo",
					},
				},
			},
		},
		Size: 1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repositories/") {
			t.Errorf("Expected repositories endpoint, got %s", r.URL.Path)
		}

		query := r.URL.Query().Get("q")
		if query != "" && !strings.Contains(query, "name~") {
			t.Errorf("Expected name query, got %s", query)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	os.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	os.Setenv("ATLASSIAN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ATLASSIAN_EMAIL")
		os.Unsetenv("ATLASSIAN_TOKEN")
	}()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	results, err := client.SearchRepositories(ctx, "workspace", "test", 10)
	if err != nil {
		t.Fatalf("SearchRepositories failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Title != "test-repo" {
		t.Errorf("Expected title 'test-repo', got %s", result.Title)
	}

	if result.ID != "workspace/test-repo" {
		t.Errorf("Expected ID 'workspace/test-repo', got %s", result.ID)
	}

	if result.Source != "bitbucket" {
		t.Errorf("Expected source 'bitbucket', got %s", result.Source)
	}

	if result.Workspace != "testowner" {
		t.Errorf("Expected workspace 'testowner', got %s", result.Workspace)
	}

	if result.Repo != "test-repo" {
		t.Errorf("Expected repo 'test-repo', got %s", result.Repo)
	}
}

func TestBitbucketClient_SearchPullRequests(t *testing.T) {
	mockResponse := PullRequestSearchResult{
		Values: []PullRequest{
			{
				ID:          42,
				Title:       "Test PR",
				Description: "This is a test pull request",
				State:       "OPEN",
				Author: User{
					DisplayName: "PR Author",
					Username:    "prauthor",
				},
				UpdatedOn: "2023-01-01T12:00:00.000000+00:00",
				Source: Branch{
					Branch: struct {
						Name string `json:"name"`
					}{
						Name: "feature-branch",
					},
					Repository: Repository{
						Name: "test-repo",
						Owner: User{
							Username: "workspace",
						},
					},
				},
				Destination: Branch{
					Branch: struct {
						Name string `json:"name"`
					}{
						Name: "main",
					},
				},
				Links: PRLinks{
					HTML: struct {
						Href string `json:"href"`
					}{
						Href: "https://bitbucket.org/workspace/test-repo/pull-requests/42",
					},
				},
			},
		},
		Size: 1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pullrequests") {
			t.Errorf("Expected pullrequests endpoint, got %s", r.URL.Path)
		}

		state := r.URL.Query().Get("state")
		if state != "OPEN" {
			t.Errorf("Expected state=OPEN, got %s", state)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	os.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	os.Setenv("ATLASSIAN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ATLASSIAN_EMAIL")
		os.Unsetenv("ATLASSIAN_TOKEN")
	}()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	results, err := client.SearchPullRequests(ctx, "workspace", "test-repo", "test", 10)
	if err != nil {
		t.Fatalf("SearchPullRequests failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Title != "Test PR" {
		t.Errorf("Expected title 'Test PR', got %s", result.Title)
	}

	if result.ID != "42" {
		t.Errorf("Expected ID '42', got %s", result.ID)
	}

	if result.Source != "bitbucket" {
		t.Errorf("Expected source 'bitbucket', got %s", result.Source)
	}

	if result.Workspace != "workspace" {
		t.Errorf("Expected workspace 'workspace', got %s", result.Workspace)
	}

	if result.Repo != "test-repo" {
		t.Errorf("Expected repo 'test-repo', got %s", result.Repo)
	}

	if !strings.Contains(result.Content, "test pull request") {
		t.Errorf("Expected content to contain 'test pull request', got %s", result.Content)
	}
}

func TestBitbucketClient_GetPullRequest(t *testing.T) {
	mockPR := PullRequest{
		ID:          123,
		Title:       "Individual PR",
		Description: "<p>This is a <strong>formatted</strong> PR description.</p>",
		State:       "OPEN",
		Author: User{
			DisplayName: "Individual Author",
			Username:    "individual",
		},
		UpdatedOn: "2023-01-02T12:00:00.000000+00:00",
		Source: Branch{
			Branch: struct {
				Name string `json:"name"`
			}{
				Name: "feature",
			},
			Repository: Repository{
				Name: "my-repo",
				Owner: User{
					Username: "myworkspace",
				},
			},
		},
		Destination: Branch{
			Branch: struct {
				Name string `json:"name"`
			}{
				Name: "main",
			},
		},
		Links: PRLinks{
			HTML: struct {
				Href string `json:"href"`
			}{
				Href: "https://bitbucket.org/myworkspace/my-repo/pull-requests/123",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/repositories/myworkspace/my-repo/pullrequests/123"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockPR)
	}))
	defer server.Close()

	os.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	os.Setenv("ATLASSIAN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ATLASSIAN_EMAIL")
		os.Unsetenv("ATLASSIAN_TOKEN")
	}()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	doc, err := client.GetPullRequest(ctx, "myworkspace", "my-repo", 123, false)
	if err != nil {
		t.Fatalf("GetPullRequest failed: %v", err)
	}

	if doc.Title != "Individual PR" {
		t.Errorf("Expected title 'Individual PR', got %s", doc.Title)
	}

	if doc.ID != "123" {
		t.Errorf("Expected ID '123', got %s", doc.ID)
	}

	if doc.Source != "bitbucket" {
		t.Errorf("Expected source 'bitbucket', got %s", doc.Source)
	}

	if doc.Workspace != "myworkspace" {
		t.Errorf("Expected workspace 'myworkspace', got %s", doc.Workspace)
	}

	if doc.Repo != "my-repo" {
		t.Errorf("Expected repo 'my-repo', got %s", doc.Repo)
	}

	expectedURL := "https://bitbucket.org/myworkspace/my-repo/pull-requests/123"
	if doc.URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, doc.URL)
	}

	if !strings.Contains(doc.Content, "formatted") {
		t.Errorf("Expected content to contain 'formatted', got %s", doc.Content)
	}

	if !strings.Contains(doc.Content, "**formatted**") {
		t.Errorf("Expected content to contain markdown formatting, got %s", doc.Content)
	}
}
