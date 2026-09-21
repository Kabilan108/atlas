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

type prTestTransport func(*http.Request) (*http.Response, error)

func (f prTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Each command constructs a new client, sharing only the on-disk cache, just as
// separate CLI invocations do. All HTTP traffic stays inside this fixture.
func TestPRDraftAndEditReadback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ATLAS_USERNAME", "test")
	t.Setenv("ATLAS_API_TOKEN", "test")
	t.Setenv("EDITOR", "/atlas-test-editor-must-not-run")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	pr := map[string]any{"id": 7, "title": "Original", "description": "Old body", "state": "OPEN", "draft": false, "source": map[string]any{"branch": map[string]string{"name": "feature"}}}
	var writes []map[string]any
	reads := 0
	failWrite := false
	http.DefaultTransport = prTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.bitbucket.org" {
			return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
		}
		status := 200
		var result any
		switch {
		case r.Method == "PUT" || r.Method == "POST":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			writes = append(writes, payload)
			if failWrite {
				status = 400
				result = map[string]any{"error": map[string]string{"message": "rejected"}}
				break
			}
			for k, v := range payload {
				pr[k] = v
			}
			result = pr
		case strings.HasSuffix(r.URL.Path, "/comments"):
			result = map[string]any{"values": []any{}}
		case strings.HasSuffix(r.URL.Path, "/pullrequests/7"):
			reads++
			result = pr
		case strings.HasSuffix(r.URL.Path, "/pullrequests"):
			reads++
			result = map[string]any{"values": []any{pr}}
		case strings.HasSuffix(r.URL.Path, "/repo"):
			result = map[string]any{"mainbranch": map[string]string{"name": "main"}}
		default:
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		body, err := json.Marshal(result)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, err
	})
	run := func(args ...string) (string, error) {
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
		err = root.Execute()
		if _, seekErr := file.Seek(0, 0); seekErr != nil {
			t.Fatal(seekErr)
		}
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return string(data), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	contains := func(out, expected string) {
		t.Helper()
		if !strings.Contains(out, expected) {
			t.Fatalf("output lacks %q: %s", expected, out)
		}
	}
	contains(must("view", "7", "-R", "ws/repo", "--json"), `"draft": false`)
	must("view", "feature", "-R", "ws/repo", "--raw")
	must("list", "-R", "ws/repo")
	before := reads
	must("view", "7", "-R", "ws/repo", "--json")
	if reads != before {
		t.Fatal("fixture did not populate disk cache")
	}
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("New body"), 0600); err != nil {
		t.Fatal(err)
	}
	must("edit", "7", "-R", "ws/repo", "-F", bodyFile, "--draft")
	if writes[len(writes)-1]["draft"] != true {
		t.Fatal("draft true missing from edit payload")
	}
	contains(must("view", "7", "-R", "ws/repo", "--json"), `"description": "New body"`)
	contains(must("view", "feature", "-R", "ws/repo", "--raw"), "draft: true")
	contains(must("list", "-R", "ws/repo"), "DRAFT")
	contains(must("list", "-R", "ws/repo", "--json"), `"draft": true`)
	must("edit", "7", "-R", "ws/repo", "--ready", "--no-cache")
	payload := writes[len(writes)-1]
	if payload["draft"] != false || len(payload) != 1 {
		t.Fatalf("ready payload = %#v", payload)
	}
	contains(must("view", "7", "-R", "ws/repo", "--json"), `"draft": false`)
	if strings.Contains(must("list", "-R", "ws/repo"), "DRAFT") {
		t.Fatal("list still shows draft")
	}
	must("edit", "7", "-R", "ws/repo", "--draft") // State-only edit must not open the editor.
	if payload := writes[len(writes)-1]; payload["draft"] != true || len(payload) != 1 {
		t.Fatalf("draft payload = %#v", payload)
	}
	for _, tc := range []struct {
		flag string
		want bool
	}{{"--draft=false", false}, {"--ready=false", true}} {
		must("edit", "7", "-R", "ws/repo", tc.flag)
		if writes[len(writes)-1]["draft"] != tc.want {
			t.Fatalf("%s payload = %#v", tc.flag, writes[len(writes)-1])
		}
	}
	// A stale cache must not suppress an explicit request matching cached state.
	must("view", "7", "-R", "ws/repo", "--raw")
	pr["draft"] = false
	must("edit", "7", "-R", "ws/repo", "--draft")
	if pr["draft"] != true {
		t.Fatal("stale cache suppressed explicit draft update")
	}
	for _, flags := range [][]string{{"--draft", "--ready"}, {"--draft=false", "--ready"}} {
		args := append([]string{"edit", "7", "-R", "ws/repo"}, flags...)
		if _, err := run(args...); err == nil {
			t.Fatalf("accepted conflicting flags %v", flags)
		}
	}
	must("edit", "7", "-R", "ws/repo", "--title", "New title")
	if _, ok := writes[len(writes)-1]["draft"]; ok {
		t.Fatal("title edit changes draft state")
	}
	contains(must("view", "7", "-R", "ws/repo", "--raw"), "draft: true")
	before = reads
	failWrite = true
	if _, err := run("edit", "7", "-R", "ws/repo", "--ready"); err == nil {
		t.Fatal("failed update reported success")
	}
	must("view", "7", "-R", "ws/repo", "--raw")
	if reads != before {
		t.Fatal("failed mutation invalidated cache")
	}
	failWrite = false
	// Give create a local remote-tracking branch, without any network git access.
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"init"}, {"-c", "user.name=Test", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "test"}, {"update-ref", "refs/remotes/origin/feature", "HEAD"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	beforeWrites := len(writes)
	contains(must("create", "-R", "ws/repo", "--head", "feature", "--title", "Draft creation", "--draft", "--dry-run"), `"draft":true`)
	if len(writes) != beforeWrites {
		t.Fatal("dry-run performed a write")
	}
	must("create", "-R", "ws/repo", "--head", "feature", "--title", "Draft creation", "--draft")
	if writes[len(writes)-1]["draft"] != true {
		t.Fatal("create payload lacks draft")
	}
	contains(must("list", "-R", "ws/repo"), "Draft creation")
	contains(must("view", "7", "-R", "ws/repo", "--json"), `"draft": true`)
	if _, err := run("create", "--draft", "--web"); err == nil {
		t.Fatal("web create silently ignored draft")
	}
}
