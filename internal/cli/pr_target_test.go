package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const otherPRURL = "https://bitbucket.org/ws/other/pull-requests/5"
const otherPRPath = "/2.0/repositories/ws/other/pullrequests/5"

// installOtherRepoTransport serves only ws/other#5 and fails any request for
// another repository, recording each request as "METHOD path".
func installOtherRepoTransport(t *testing.T) *[]string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ATLAS_USERNAME", "test")
	t.Setenv("ATLAS_API_TOKEN", "test")
	t.Setenv("ATLAS_WORKSPACE", "")
	t.Setenv("EDITOR", "/atlas-test-editor-must-not-run")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	pr := map[string]any{
		"id": 5, "title": "Other PR", "description": "body", "state": "OPEN",
		"source":      map[string]any{"branch": map[string]string{"name": "feature"}},
		"destination": map[string]any{"branch": map[string]string{"name": "main"}},
	}
	var requests []string
	http.DefaultTransport = prTestTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.URL.Host != "api.bitbucket.org" {
			return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
		}
		if r.URL.Path != otherPRPath && !strings.HasPrefix(r.URL.Path, otherPRPath+"/") {
			return nil, fmt.Errorf("request sent to wrong repository: %s %s", r.Method, r.URL.Path)
		}
		suffix := strings.TrimPrefix(r.URL.Path, otherPRPath)
		var body []byte
		var err error
		switch {
		case r.Method == "GET" && suffix == "":
			body, err = json.Marshal(pr)
		case r.Method == "PUT" && suffix == "", r.Method == "POST" && suffix == "/decline":
			body, err = json.Marshal(pr)
		case r.Method == "GET" && (suffix == "/comments" || suffix == "/tasks"):
			body = []byte(`{"values":[]}`)
		case r.Method == "GET" && suffix == "/diff":
			body = []byte("diff --git a/a.go b/a.go\n")
		case r.Method == "POST" && suffix == "/comments":
			body = []byte(`{"id":1}`)
		case r.Method == "POST" && (suffix == "/approve" || suffix == "/request-changes"):
			body = []byte(`{"approved":true}`)
		case r.Method == "DELETE" && (suffix == "/approve" || suffix == "/request-changes"):
			body = []byte(`{}`)
		default:
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, err
	})
	return &requests
}

func runPRCommand(t *testing.T, args ...string) error {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = original; file.Close() }()
	root := NewRootCmd("test")
	root.PersistentPreRun = nil
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs(append([]string{"pr"}, args...))
	return root.Execute()
}

func initGitRepoWithOrigin(t *testing.T, origin string) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
}

func prURLTargetCommands(t *testing.T) [][]string {
	specFile := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(specFile, []byte(`[{"body":"inline","path":"a.go","line":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	return [][]string{
		{"approve", otherPRURL, "-b", "lgtm"},
		{"request-changes", otherPRURL},
		{"unapprove", otherPRURL},
		{"clear-change-request", otherPRURL},
		{"comment", otherPRURL, "-b", "hello"},
		{"review", otherPRURL, "--approve", "-b", "summary", "--comment-spec", specFile},
		{"edit", otherPRURL, "--title", "Renamed"},
		{"close", otherPRURL, "-c", "closing"},
		{"view", otherPRURL, "--raw", "--comments"},
		{"view", otherPRURL, "--json"},
		{"diff", otherPRURL, "--patch"},
	}
}

func assertRequests(t *testing.T, requests []string, want ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, request := range requests {
		seen[request] = true
	}
	for _, request := range want {
		if !seen[request] {
			t.Errorf("missing request %q; got %v", request, requests)
		}
	}
}

func TestPRURLSelectorTargetsURLRepository(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{name: "inferred checkout repo"},
		{name: "explicit -R repo", extra: []string{"-R", "ws/repo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := installOtherRepoTransport(t)
			initGitRepoWithOrigin(t, "git@bitbucket.org:ws/repo.git")
			for _, args := range prURLTargetCommands(t) {
				if err := runPRCommand(t, append(args, tc.extra...)...); err != nil {
					t.Fatalf("%v: %v", args, err)
				}
			}
			assertRequests(t, *requests,
				"POST "+otherPRPath+"/approve",
				"POST "+otherPRPath+"/request-changes",
				"DELETE "+otherPRPath+"/approve",
				"DELETE "+otherPRPath+"/request-changes",
				"POST "+otherPRPath+"/comments",
				"PUT "+otherPRPath,
				"POST "+otherPRPath+"/decline",
				"GET "+otherPRPath+"/tasks",
				"GET "+otherPRPath+"/diff",
			)
		})
	}
}

func TestPRURLSelectorWorksOutsideGitRepository(t *testing.T) {
	requests := installOtherRepoTransport(t)
	t.Chdir(t.TempDir())
	for _, args := range prURLTargetCommands(t) {
		if err := runPRCommand(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	assertRequests(t, *requests, "POST "+otherPRPath+"/approve", "POST "+otherPRPath+"/comments")
	if err := runPRCommand(t, "approve", "5"); err == nil || !strings.Contains(err.Error(), "could not infer repository") {
		t.Fatalf("number selector outside a git repo: err = %v", err)
	}
}
