package cli

import (
	"bytes"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

var updateGolden = flag.Bool("update", false, "rewrite prtest golden files from current CLI output")

var prViewGoldenCases = []struct {
	name   string
	golden string
	args   []string
}{
	{"number", "view.md", []string{"view", "42", "-R", "acme/widgets", "--raw"}},
	{"comments", "view_comments.md", []string{"view", "42", "-R", "acme/widgets", "--raw", "-c"}},
	{"comments all", "view_comments_all.md", []string{"view", "42", "-R", "acme/widgets", "--raw", "-c", "--all"}},
	{"json", "view.json", []string{"view", "42", "-R", "acme/widgets", "--json"}},
	{"json ignores comments flag", "view.json", []string{"view", "42", "-R", "acme/widgets", "--json", "-c"}},
	{"branch", "view_comments.md", []string{"view", "feature/login", "-R", "acme/widgets", "--raw", "--comments"}},
	{"url with repo flag", "view_comments.md", []string{"view", prtest.URL, "-R", "acme/widgets", "--raw", "-c"}},
}

// setupCLIEnv isolates config, credentials, and cache from the developer's
// machine and sets fake credentials.
func setupCLIEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ATLAS_WORKSPACE", "")
	t.Setenv("ATLAS_USERNAME", "cli-user")
	t.Setenv("ATLAS_API_TOKEN", "cli-token")
	t.Setenv("ATLAS_JIRA_TOKEN", "")
}

// runAtlas runs the root command in-process against fixture and returns what
// it wrote to stdout.
func runAtlas(t *testing.T, fixture http.RoundTripper, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: fixture}))
	root.PersistentPreRun = nil
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(&stdout)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), err
}

func TestPRViewMatchesGolden(t *testing.T) {
	updated := map[string]string{}
	for _, tc := range prViewGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			setupCLIEnv(t)
			got, err := runAtlas(t, prtest.New(), append([]string{"--no-cache", "pr"}, tc.args...)...)
			if err != nil {
				t.Fatalf("atlas pr %v: %v", tc.args, err)
			}
			if *updateGolden && updated[tc.golden] == "" {
				updated[tc.golden] = got
				path := filepath.Join("..", "ops", "pr", "prtest", prtest.GoldenPath(tc.golden))
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, ok := updated[tc.golden]
			if !ok {
				want = prtest.Golden(tc.golden)
			}
			if got != want {
				t.Fatalf("atlas pr %v output differs from %s\n--- got ---\n%s\n--- want ---\n%s", tc.args, tc.golden, got, want)
			}
		})
	}
}

// A URL names its repository, so view must not need -R or a git checkout, and
// must not let -R redirect calls to another repository.
func TestPRViewURLNeedsNoGitAndKeepsItsRepository(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "view", prtest.URL, "--raw", "-c"},
		{"pr", "view", prtest.URL, "-R", "other/elsewhere", "--raw", "-c"},
	} {
		fixture := prtest.New()
		setupCLIEnv(t)
		t.Chdir(t.TempDir())
		got, err := runAtlas(t, fixture, append([]string{"--no-cache"}, args...)...)
		if err != nil {
			t.Fatalf("atlas %v: %v", args, err)
		}
		if want := prtest.Golden("view_comments.md"); got != want {
			t.Fatalf("atlas %v output differs from view_comments.md\n--- got ---\n%s", args, got)
		}
		for _, req := range fixture.Requests() {
			if strings.HasPrefix(req.Path, "/2.0/repositories/") && !strings.HasPrefix(req.Path, "/2.0/repositories/acme/widgets/") {
				t.Fatalf("atlas %v requested %s", args, req.Path)
			}
		}
	}
}
