package mcpserver_test

import (
	"bytes"
	"net/http"
	"slices"
	"testing"

	"github.com/kabilan108/atlas/internal/cli"
	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

// The MCP tool and `atlas pr view --raw` come from one op, so on the same
// data they must print the same bytes and make the same Bitbucket calls.
func TestPRViewParityWithCLI(t *testing.T) {
	tests := []struct {
		name string
		cli  []string
		args map[string]any
	}{
		{"plain", []string{"42"}, map[string]any{"repo": "acme/widgets", "pr": "42"}},
		{"comments", []string{"42", "-c"}, map[string]any{"repo": "acme/widgets", "pr": "42", "comments": true}},
		{"resolved", []string{"42", "-c", "--all"}, map[string]any{"repo": "acme/widgets", "pr": "42", "comments": true, "include_resolved": true}},
		{"branch", []string{prtest.Branch, "-c"}, map[string]any{"repo": "acme/widgets", "pr": prtest.Branch, "comments": true}},
		{"url", []string{prtest.URL, "-c"}, map[string]any{"pr": prtest.URL, "comments": true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := startServer(t, mcpserver.Options{})
			mcpText, isError := callText(t, connect(t, srv.url, credentials()), "pr_view", tc.args)
			if isError {
				t.Fatal(mcpText)
			}

			cliFixture := prtest.New()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("ATLAS_WORKSPACE", "")
			t.Setenv("ATLAS_USERNAME", "cli-user")
			t.Setenv("ATLAS_API_TOKEN", "cli-token")
			args := append([]string{"--no-cache", "pr", "view"}, tc.cli...)
			if tc.name != "url" {
				args = append(args, "-R", "acme/widgets")
			}
			var stdout bytes.Buffer
			root := cli.NewRootCmd("test", cli.WithHTTPClient(&http.Client{Transport: cliFixture}))
			root.PersistentPreRun = nil
			root.SetOut(&stdout)
			root.SetArgs(append(args, "--raw"))
			if err := root.Execute(); err != nil {
				t.Fatalf("atlas %v: %v", args, err)
			}

			if mcpText != stdout.String() {
				t.Fatalf("MCP and CLI differ\n--- mcp ---\n%s\n--- cli ---\n%s", mcpText, stdout.String())
			}
			if got, want := paths(srv.fixture), paths(cliFixture); !slices.Equal(got, want) {
				t.Fatalf("Bitbucket calls differ\nmcp: %v\ncli: %v", got, want)
			}
		})
	}
}

func paths(fixture *prtest.Fixture) []string {
	var out []string
	for _, req := range fixture.Requests() {
		out = append(out, req.Method+" "+req.Path)
	}
	return out
}
