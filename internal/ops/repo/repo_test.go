package repo_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/repo"
	"github.com/kabilan108/atlas/internal/ops/repo/repotest"
)

func newEnv(t *testing.T) (*opstest.Env, *repotest.Fixture) {
	t.Helper()
	fixture := repotest.New()
	return opstest.New(repotest.Workspace).WithBitbucket(t, fixture), fixture
}

func invoke(t *testing.T, def ops.Def, args string) (ops.Result, error) {
	t.Helper()
	env, _ := newEnv(t)
	return def.Invoke(context.Background(), env, json.RawMessage(args))
}

func TestFileReadBatchReportsEachRepository(t *testing.T) {
	env, fixture := newEnv(t)
	res, err := repo.FileRead.Invoke(context.Background(), env, json.RawMessage(`{"repos":["*","widgets"],"path":"version.json"}`))
	if ops.KindOf(err) != ops.Partial || !strings.Contains(err.Error(), "1 of 4 files failed") {
		t.Fatalf("err = %v, want Partial for 1 of 4", err)
	}
	result := res.Value.(*repo.FileReadResult)
	var got []string
	for _, entry := range result.Results {
		got = append(got, entry.Repo+":"+string(entry.Status))
	}
	if want := []string{"acme/widgets:ok", "acme/gadgets:ok", "acme/legacy:failed", "acme/blob:ok"}; !slices.Equal(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	for i, entry := range result.Results {
		if entry.Index != i {
			t.Fatalf("entry %d has index %d", i, entry.Index)
		}
	}
	if !result.Results[3].Binary || result.Results[3].Content != "" {
		t.Fatalf("blob entry = %+v, want binary without content", result.Results[3])
	}
	want := "version.json at main branch: read from 3 of 4 repositories\n" +
		"\n## acme/widgets\n\n```json\n{\"version\":\"1.2.3\"}\n```\n" +
		"\n## acme/gadgets\n\n```json\n{\"version\":\"2.0.0\"}\n```\n" +
		"\n## acme/legacy\n\nError: file: file 'HEAD:version.json' not found\n" +
		"\n## acme/blob\n\nBinary file, 8 bytes; not shown.\n"
	if res.Text != want {
		t.Fatalf("markdown =\n%q\nwant\n%q", res.Text, want)
	}
	if n := strings.Count(strings.Join(fixture.Requests(), "\n"), "/src/HEAD/version.json"); n != 4 {
		t.Fatalf("read the file %d times, want once per distinct repo", n)
	}
}

func TestFileReadSingleRepo(t *testing.T) {
	res, err := invoke(t, repo.FileRead, `{"repo":"widgets","path":"/version.json","ref":"v1","max_bytes":5}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "version.json in acme/widgets at v1\n\n```json\n{\"ver\n```\nTruncated at 5 bytes; raise max_bytes for more.\n"
	if res.Text != want {
		t.Fatalf("markdown = %q, want %q", res.Text, want)
	}
	if _, err := invoke(t, repo.FileRead, `{"repo":"legacy","path":"version.json"}`); ops.KindOf(err) != ops.NotFound {
		t.Fatalf("missing file err = %v, want NotFound", err)
	}
}

func TestFileReadRejectsBadInput(t *testing.T) {
	for _, args := range []string{
		`{"repo":"widgets","path":""}`,
		`{"repo":"widgets","repos":["gadgets"],"path":"version.json"}`,
		`{"repos":["acme/../x"],"path":"version.json"}`,
		`{"repos":["a/b/c"],"path":"version.json"}`,
	} {
		if _, err := invoke(t, repo.FileRead, args); ops.KindOf(err) != ops.Invalid {
			t.Errorf("%s: err = %v, want Invalid", args, err)
		}
	}
}

func TestListsRenderTables(t *testing.T) {
	tests := []struct {
		def  ops.Def
		args string
		want string
		req  string
	}{
		{repo.List, `{"limit":2}`,
			"| Repo | Main branch | Visibility | Updated | Description |\n| --- | --- | --- | --- | --- |\n" +
				"| widgets | main | private | 2024-06-20 | The widgets service |\n| gadgets | main | public | 2024-06-19 | The gadgets service |\n" +
				"\nShowing the first 2; raise limit or narrow the filter for more.\n",
			"GET /2.0/repositories/acme?pagelen=3&sort=-updated_on"},
		{repo.BranchList, `{"repo":"acme/widgets","filter":"log"}`,
			"| Branch | Head | Date | Author | Message |\n| --- | --- | --- | --- | --- |\n" +
				"| main | a1b2c3d4e5f6 | 2024-06-01 12:30 | Ada Lovelace | Release 1.2.3 |\n| feature/login | 0f0e0d0c0b0a | 2024-06-01 12:30 | Ada Lovelace | Add login |\n",
			"GET /2.0/repositories/acme/widgets/refs/branches?pagelen=31&q=name+~+%22log%22&sort=-target.date"},
		{repo.CommitList, `{"repo":"widgets","ref":"feature/login","base":"main","limit":5}`,
			"Commits in acme/widgets main..feature/login\n\n| Commit | Date | Author | Message |\n| --- | --- | --- | --- |\n" +
				"| a1b2c3d4e5f6 | 2024-06-01 12:30 | Ada Lovelace | Release 1.2.3 |\n| 998877665544 | 2024-06-01 12:30 | Ada Lovelace | Fix \\| pipes |\n",
			"GET /2.0/repositories/acme/widgets/commits?exclude=main&include=feature%2Flogin&pagelen=6"},
		{repo.UserSearch, `{"query":"grace"}`,
			"| Name | Nickname | Account ID | UUID |\n| --- | --- | --- | --- |\n| Grace Hopper | grace | 557058:grace | {grace} |\n",
			"GET /2.0/workspaces/acme/members?pagelen=100"},
		{repo.Whoami, `{}`,
			"Display name: Ada Lovelace\nNickname: ada\nAccount ID: 557058:ada\nUUID: {ada}\n",
			"GET /2.0/user"},
	}
	for _, tc := range tests {
		t.Run(tc.def.Info().Name, func(t *testing.T) {
			env, fixture := newEnv(t)
			res, err := tc.def.Invoke(context.Background(), env, json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tc.want {
				t.Fatalf("markdown =\n%q\nwant\n%q", res.Text, tc.want)
			}
			if got := fixture.Requests(); len(got) != 1 || got[0] != tc.req {
				t.Fatalf("requests = %v, want [%s]", got, tc.req)
			}
		})
	}
}

func TestBranchView(t *testing.T) {
	res, err := invoke(t, repo.BranchView, `{"repo":"widgets","branch":"main"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "Branch main in acme/widgets\n\n- Head: a1b2c3d4e5f6a7b8 (2024-06-01 12:30, Ada Lovelace)\n- Message: Release 1.2.3\n" +
		"- Default merge strategy: squash\n- Allowed merge strategies: merge_commit, squash\n"
	if res.Text != want {
		t.Fatalf("markdown = %q, want %q", res.Text, want)
	}
	res, err = invoke(t, repo.BranchView, `{"repo":"widgets","branch":"gone"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Branch gone does not exist in acme/widgets.\n" || res.Value.(*repo.BranchViewResult).Exists {
		t.Fatalf("missing branch = %q", res.Text)
	}
}

func TestReadOpsAreReadOnly(t *testing.T) {
	for _, def := range repo.Ops() {
		if meta := def.Info(); !meta.ReadOnly || meta.Destructive || meta.Surfaces != ops.Both {
			t.Errorf("%s: %+v, want read-only on both surfaces", meta.Name, meta)
		}
	}
}
