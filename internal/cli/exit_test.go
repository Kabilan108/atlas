package cli

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type respondFunc func(*http.Request) (int, string)

func (f respondFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	status, body := f(r)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func always(status int) respondFunc {
	return func(*http.Request) (int, string) {
		return status, `{"type":"error","error":{"message":"nope"},"errorMessages":["nope"]}`
	}
}

func statuses(state string) respondFunc {
	return func(*http.Request) (int, string) {
		return http.StatusOK, `{"values":[{"key":"build","name":"Build","state":"` + state + `","updated_on":"2024-06-01T12:00:00Z"}]}`
	}
}

func TestExitCodes(t *testing.T) {
	user := `{"display_name":"Ada","account_id":"557058:ada","uuid":"{ada}"}`
	tests := []struct {
		name    string
		args    []string
		respond respondFunc
		jira    bool
		want    int
		stdout  string
	}{
		{"success", []string{"whoami"}, func(*http.Request) (int, string) { return 200, user }, false, ExitOK, "Ada"},
		{"unknown command", []string{"bogus"}, nil, false, ExitUsage, ""},
		{"unknown subcommand", []string{"pr", "bogus"}, nil, false, ExitUsage, ""},
		{"unknown flag", []string{"pr", "view", "42", "--nope"}, nil, false, ExitUsage, ""},
		{"missing argument", []string{"branch", "view"}, nil, false, ExitUsage, ""},
		{"conflicting flags", []string{"snippet", "list", "--workspace", "acme", "--public", "--private"}, nil, false, ExitUsage, ""},
		{"limit over the maximum", []string{"branch", "list", "-R", "acme/widgets", "-L", "5000"}, nil, false, ExitUsage, ""},
		{"bitbucket 400", []string{"whoami"}, always(400), false, ExitUsage, ""},
		{"bitbucket 401", []string{"whoami"}, always(401), false, ExitAuth, ""},
		{"bitbucket 403", []string{"whoami"}, always(403), false, ExitAuth, ""},
		{"bitbucket 404", []string{"branch", "list", "-R", "acme/widgets"}, always(404), false, ExitNotFound, ""},
		{"bitbucket 409", []string{"whoami"}, always(409), false, ExitConflict, ""},
		{"bitbucket 429", []string{"whoami"}, always(429), false, ExitRateLimited, ""},
		{"bitbucket 502", []string{"whoami"}, always(502), false, ExitError, ""},
		{"branch missing", []string{"branch", "view", "gone", "-R", "acme/widgets"}, always(404), false, ExitNotFound, ""},
		{"partial batch prints first", []string{"file", "read", "version.json", "--repos", "acme/ok,acme/broken"}, func(r *http.Request) (int, string) {
			if strings.Contains(r.URL.Path, "/broken/") {
				return 502, `{"type":"error","error":{"message":"down"}}`
			}
			return 200, `{"version":"1.2.3"}`
		}, false, ExitPartial, "1.2.3"},
		{"checks failed", []string{"pr", "checks", "--commit", "abc123", "-R", "acme/widgets"}, statuses("FAILED"), false, ExitError, "Build"},
		{"checks pending", []string{"pr", "checks", "--commit", "abc123", "-R", "acme/widgets"}, statuses("INPROGRESS"), false, ExitPending, "Build"},
		{"jira without a token", []string{"jira", "whoami"}, nil, false, ExitAuth, ""},
		{"jira 401", []string{"jira", "whoami"}, always(401), true, ExitAuth, ""},
		{"jira 404", []string{"jira", "view", "CSM-1"}, always(404), true, ExitNotFound, ""},
		{"jira 429", []string{"jira", "whoami"}, always(429), true, ExitRateLimited, ""},
		{"jira 400", []string{"jira", "whoami"}, always(400), true, ExitUsage, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupCLIEnv(t)
			if tc.jira {
				t.Setenv("ATLAS_JIRA_TOKEN", "cli-jira-token")
				t.Setenv("ATLAS_JIRA_CLOUD_ID", "cloud-1")
			}
			respond := tc.respond
			if respond == nil {
				respond = func(r *http.Request) (int, string) {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					return 500, "{}"
				}
			}
			var stdout bytes.Buffer
			root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: respond}))
			root.PersistentPreRun = nil
			root.SetOut(&stdout)
			root.SetErr(io.Discard)
			root.SetArgs(append([]string{"--no-cache"}, tc.args...))
			err := root.Execute()
			if got := ExitCode(err); got != tc.want {
				t.Fatalf("exit code %d, want %d (err: %v)", got, tc.want, err)
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Fatalf("stdout %q, want it to contain %q", stdout.String(), tc.stdout)
			}
		})
	}
}

func TestHelpDocumentsExitCodes(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&stdout)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Exit codes:", "3  authentication", "4  not found", "7  partial", "8  timeout or still pending"} {
		if !strings.Contains(stdout.String(), line) {
			t.Fatalf("atlas --help lacks %q:\n%s", line, stdout.String())
		}
	}
}
