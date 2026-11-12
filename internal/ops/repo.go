package ops

import (
	"strings"
	"unicode"
)

// AllRepos as the repo part ("acme/*", or "*" for the default workspace)
// asks ops that accept it to cover every repository in the workspace.
const AllRepos = "*"

// RepoRef is the input that names a Bitbucket repository. Every Bitbucket op
// embeds it.
type RepoRef struct {
	Repo string `json:"repo,omitempty" jsonschema:"workspace/repo or bare repo name (default workspace)"`
}

// Repository is a validated workspace and repository slug. An empty Repo
// means every repository in Workspace.
type Repository struct {
	Workspace string
	Repo      string
}

// All reports whether r covers every repository in its workspace.
func (r Repository) All() bool {
	return r.Repo == ""
}

func (r Repository) String() string {
	if r.All() {
		return r.Workspace + "/" + AllRepos
	}
	return r.Workspace + "/" + r.Repo
}

// Resolve validates ref.Repo and fills in the default workspace for a bare
// repo. It names exactly one repository.
func (ref RepoRef) Resolve(env Env) (Repository, error) {
	repo, err := ref.ResolveScope(env)
	if err != nil {
		return Repository{}, err
	}
	if repo.All() {
		return Repository{}, Errorf(Invalid, "repo %q names every repository; give one repo", ref.Repo)
	}
	return repo, nil
}

// ResolveScope is Resolve for ops that can also cover a whole workspace:
// "acme/*" means every repository in acme and "*" every repository in the
// default workspace.
func (ref RepoRef) ResolveScope(env Env) (Repository, error) {
	raw := strings.TrimSpace(ref.Repo)
	if raw == "" {
		return Repository{}, Errorf(Invalid, "repo is required: give workspace/repo or a repo in the default workspace")
	}
	workspace, repo, qualified := strings.Cut(raw, "/")
	if !qualified {
		workspace, repo = "", raw
	}
	if strings.Contains(repo, "/") {
		return Repository{}, Errorf(Invalid, "invalid repository %q: expected repo or workspace/repo", ref.Repo)
	}
	if !qualified {
		workspace = env.DefaultWorkspace()
		if workspace == "" {
			return Repository{}, Errorf(Invalid, "no default workspace: give repo as workspace/repo")
		}
	}
	if err := ValidateSlug("workspace", workspace); err != nil {
		return Repository{}, err
	}
	if repo == AllRepos {
		return Repository{Workspace: workspace}, nil
	}
	if err := ValidateSlug("repository", repo); err != nil {
		return Repository{}, err
	}
	return Repository{Workspace: workspace, Repo: repo}, nil
}

// Workspace validates raw as a workspace slug, or returns the default
// workspace when raw is empty. Workspace-level ops (snippets, members) use it.
func Workspace(raw string, env Env) (string, error) {
	workspace := strings.TrimSpace(raw)
	if workspace == "" {
		workspace = env.DefaultWorkspace()
		if workspace == "" {
			return "", Errorf(Invalid, "workspace is required: there is no default workspace")
		}
	}
	if err := ValidateSlug("workspace", workspace); err != nil {
		return "", err
	}
	return workspace, nil
}

// ValidateSlug rejects a workspace or repository name that could leave its
// API path segment or reach another endpoint: empty, containing a slash,
// backslash, ?, #, %, "..", or a control character, or exactly ".". Clients
// still escape every segment; this turns bad input into a clear error.
func ValidateSlug(kind, value string) error {
	switch {
	case value == "":
		return Errorf(Invalid, "%s name is empty", kind)
	case value == ".", strings.Contains(value, ".."):
		return Errorf(Invalid, "invalid %s %q: dot segments are not allowed", kind, value)
	case strings.ContainsAny(value, `/\?#%`):
		return Errorf(Invalid, "invalid %s %q: must not contain / \\ ? # or %%", kind, value)
	case strings.ContainsFunc(value, unicode.IsControl):
		return Errorf(Invalid, "invalid %s %q: must not contain control characters", kind, value)
	}
	return nil
}
