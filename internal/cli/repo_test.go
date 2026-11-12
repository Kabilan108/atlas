package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/repo/repotest"
)

func runRepoCmd(t *testing.T, args ...string) (string, error, []string) {
	t.Helper()
	setupCLIEnv(t)
	t.Setenv("ATLAS_WORKSPACE", repotest.Workspace)
	t.Chdir(t.TempDir())
	fixture := repotest.New()
	out, err := runAtlas(t, fixture, append([]string{"--no-cache"}, args...)...)
	return out, err, fixture.Requests()
}

func TestFileReadPrintsOneFileAsIs(t *testing.T) {
	out, err, _ := runRepoCmd(t, "file", "read", "version.json", "-R", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"version":"1.2.3"}` {
		t.Fatalf("stdout = %q, want the raw file", out)
	}
}

func TestFileReadBatchPrintsSectionsThenFails(t *testing.T) {
	out, err, _ := runRepoCmd(t, "file", "read", "version.json", "--repos", "widgets,legacy")
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want Partial", err)
	}
	if !strings.Contains(out, "## acme/widgets\n\n```json\n{\"version\":\"1.2.3\"}") || !strings.Contains(out, "## acme/legacy\n\nError:") {
		t.Fatalf("stdout = %q", out)
	}

	out, err, _ = runRepoCmd(t, "file", "read", "version.json", "--repos", "widgets", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Results []struct {
			Index   int    `json:"index"`
			Status  string `json:"status"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || len(result.Results) != 1 || result.Results[0].Status != "ok" {
		t.Fatalf("json = %s (%v)", out, err)
	}
}

func TestBranchViewFailsForMissingBranch(t *testing.T) {
	out, err, _ := runRepoCmd(t, "branch", "view", "gone", "-R", "acme/widgets")
	if ops.KindOf(err) != ops.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
	if out != "Branch gone does not exist in acme/widgets.\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRepoCommandsUseFlags(t *testing.T) {
	tests := []struct {
		args []string
		req  string
		out  string
	}{
		{[]string{"repo", "list", "-L", "1", "--name", "gad"}, "GET /2.0/repositories/acme?pagelen=2&q=name+~+%22gad%22&sort=-updated_on", "| gadgets | main | public |"},
		{[]string{"branch", "list", "-R", "widgets", "-L", "1"}, "GET /2.0/repositories/acme/widgets/refs/branches?pagelen=2&sort=-target.date", "| main | a1b2c3d4e5f6 |"},
		{[]string{"commit", "list", "main", "-R", "widgets", "--path", "src"}, "GET /2.0/repositories/acme/widgets/commits/main?pagelen=31&path=src", "Commits in acme/widgets main"},
		{[]string{"user", "search", "ada"}, "GET /2.0/workspaces/acme/members?pagelen=100", "| Ada Lovelace | ada | 557058:ada | {ada} |"},
		{[]string{"whoami"}, "GET /2.0/user", "Account ID: 557058:ada"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, err, reqs := runRepoCmd(t, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(reqs) != 1 || reqs[0] != tc.req {
				t.Fatalf("requests = %v, want [%s]", reqs, tc.req)
			}
			if !strings.Contains(out, tc.out) {
				t.Fatalf("stdout = %q, want it to contain %q", out, tc.out)
			}
		})
	}
}
