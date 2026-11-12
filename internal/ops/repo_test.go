package ops_test

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

func TestRepoRefResolve(t *testing.T) {
	tests := []struct {
		repo, defaultWorkspace string
		want                   ops.Repository
		err                    string
	}{
		{"acme/widgets", "", ops.Repository{Workspace: "acme", Repo: "widgets"}, ""},
		{" widgets ", "acme", ops.Repository{Workspace: "acme", Repo: "widgets"}, ""},
		{"other/widgets", "acme", ops.Repository{Workspace: "other", Repo: "widgets"}, ""},
		{"my.repo_1-x", "acme", ops.Repository{Workspace: "acme", Repo: "my.repo_1-x"}, ""},
		{"widgets", "", ops.Repository{}, "no default workspace"},
		{"", "acme", ops.Repository{}, "repo is required"},
		{"a/b/c", "", ops.Repository{}, "invalid repository"},
		{"/widgets", "", ops.Repository{}, "workspace name is empty"},
		{"acme/", "", ops.Repository{}, "repository name is empty"},
		{"acme/widgets?fields=description#", "", ops.Repository{}, "must not contain"},
		{"acme/wid#gets", "", ops.Repository{}, "must not contain"},
		{"acme/wid%2Fgets", "", ops.Repository{}, "must not contain"},
		{`acme/wid\gets`, "", ops.Repository{}, "must not contain"},
		{"acme/..", "", ops.Repository{}, "dot segments"},
		{"../widgets", "", ops.Repository{}, "dot segments"},
		{"acme/.", "", ops.Repository{}, "dot segments"},
		{"acme/wid\x00gets", "", ops.Repository{}, "control characters"},
		{"acme/wid\ngets", "", ops.Repository{}, "control characters"},
		{"widgets", "bad?ws", ops.Repository{}, "invalid workspace"},
		{"acme/*", "", ops.Repository{}, "names every repository"},
	}
	for _, tc := range tests {
		got, err := ops.RepoRef{Repo: tc.repo}.Resolve(opstest.New(tc.defaultWorkspace))
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) || ops.KindOf(err) != ops.Invalid {
				t.Errorf("Resolve(%q) error = %v, want invalid %q", tc.repo, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Resolve(%q) = %+v, %v; want %+v", tc.repo, got, err, tc.want)
		}
	}
}

func TestRepoRefResolveScopeCoversWorkspace(t *testing.T) {
	for repo, want := range map[string]ops.Repository{
		"acme/*":       {Workspace: "acme"},
		"*":            {Workspace: "dflt"},
		"acme/widgets": {Workspace: "acme", Repo: "widgets"},
	} {
		got, err := ops.RepoRef{Repo: repo}.ResolveScope(opstest.New("dflt"))
		if err != nil || got != want {
			t.Errorf("ResolveScope(%q) = %+v, %v; want %+v", repo, got, err, want)
		}
		if got.All() != (want.Repo == "") {
			t.Errorf("ResolveScope(%q).All() = %v", repo, got.All())
		}
	}
	if got := (ops.Repository{Workspace: "acme"}).String(); got != "acme/*" {
		t.Errorf("String() = %q", got)
	}
}

func TestWorkspace(t *testing.T) {
	if got, err := ops.Workspace("", opstest.New("dflt")); err != nil || got != "dflt" {
		t.Errorf("default: %q, %v", got, err)
	}
	if got, err := ops.Workspace("acme", opstest.New("")); err != nil || got != "acme" {
		t.Errorf("explicit: %q, %v", got, err)
	}
	if _, err := ops.Workspace("", opstest.New("")); ops.KindOf(err) != ops.Invalid {
		t.Errorf("missing: %v", err)
	}
	if _, err := ops.Workspace("ac/me", opstest.New("")); ops.KindOf(err) != ops.Invalid {
		t.Errorf("slash: %v", err)
	}
}
