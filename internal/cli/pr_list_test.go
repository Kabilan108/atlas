package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// A repository that fails no longer hides behind an empty list: the PRs from
// the rest are printed and the command still fails, naming the repository.
func TestPRListAllPrintsPartialResultsThenFails(t *testing.T) {
	setupCLIEnv(t)
	t.Setenv("ATLAS_WORKSPACE", "ws")
	transport := prTestTransport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{"values":[{"id":3,"title":"ok"}]}`
		switch r.URL.Path {
		case "/2.0/repositories/ws":
			body = `{"values":[{"name":"a","full_name":"ws/a"},{"name":"b","full_name":"ws/b"}]}`
		case "/2.0/repositories/ws/b/pullrequests":
			status, body = http.StatusForbidden, `{}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})

	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: transport}))
	root.PersistentPreRun = nil
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{"--no-cache", "pr", "list", "--all", "--json"})
	runErr := root.Execute()
	os.Stdout = original
	file.Close()

	if runErr == nil || !strings.Contains(runErr.Error(), "1 of 2 repositories") || !strings.Contains(runErr.Error(), "\n  b: ") {
		t.Fatalf("error = %v, want the failed repository named", runErr)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var prs []map[string]any
	if err := json.Unmarshal(data, &prs); err != nil || len(prs) != 1 {
		t.Fatalf("stdout = %s (%v), want the one PR from ws/a", data, err)
	}
}
