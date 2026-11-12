package cli

import (
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// setupCLIFixture points every Bitbucket client at fixture and isolates
// config, credentials, and cache from the developer's machine.
func setupCLIFixture(t *testing.T, fixture *prtest.Fixture) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ATLAS_WORKSPACE", "")
	t.Setenv("ATLAS_USERNAME", "cli-user")
	t.Setenv("ATLAS_API_TOKEN", "cli-token")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = fixture
}

// runAtlas runs the root command in-process and returns what it wrote to stdout.
func runAtlas(t *testing.T, args ...string) (string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	original := os.Stdout
	os.Stdout = file
	root := NewRootCmd("test")
	root.PersistentPreRun = nil
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout = original
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func TestPRViewMatchesGolden(t *testing.T) {
	updated := map[string]string{}
	for _, tc := range prViewGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			setupCLIFixture(t, prtest.New())
			got, err := runAtlas(t, append([]string{"--no-cache", "pr"}, tc.args...)...)
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
