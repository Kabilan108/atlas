// Package pr holds the pull request operations.
package pr

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// Ref is the input that names a pull request. Every PR op embeds it.
type Ref struct {
	Repo string `json:"repo,omitempty" jsonschema:"workspace/repo, or repo in the default workspace; omit when pr is a URL"`
	PR   string `json:"pr" jsonschema:"PR number, Bitbucket PR URL, or source branch name"`

	// CurrentBranch is used when PR is empty. Only the CLI sets it, from git.
	CurrentBranch string `json:"-"`
}

// Selector is a parsed PR argument. A URL fills Workspace and Repo.
type Selector struct {
	Workspace string
	Repo      string
	ID        int
	Branch    string
}

// IsURL reports whether the selector came from a PR URL, which names its own
// repository.
func (s Selector) IsURL() bool {
	return s.Workspace != ""
}

// IsEmpty reports whether the selector names no PR at all.
func (s Selector) IsEmpty() bool {
	return s.ID == 0 && s.Branch == "" && !s.IsURL()
}

// ParseSelector accepts a PR number (optionally prefixed with #), a
// bitbucket.org pull request URL, or a branch name.
func ParseSelector(raw string) (Selector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Selector{}, nil
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host == "bitbucket.org" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull-requests" {
			id, err := strconv.Atoi(parts[3])
			if err != nil || id <= 0 {
				return Selector{}, ops.Errorf(ops.Invalid, "invalid pull request URL %q", raw)
			}
			return Selector{Workspace: parts[0], Repo: parts[1], ID: id}, nil
		}
	}
	if id, err := strconv.Atoi(strings.TrimPrefix(raw, "#")); err == nil {
		if id <= 0 {
			return Selector{}, ops.Errorf(ops.Invalid, "invalid pull request number %q", raw)
		}
		return Selector{ID: id}, nil
	}
	return Selector{Branch: raw}, nil
}

// SplitRepo parses "workspace/repo" or a bare "repo" in defaultWorkspace.
func SplitRepo(raw, defaultWorkspace string) (workspace, repo string, err error) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		if defaultWorkspace == "" {
			return "", "", ops.Errorf(ops.Invalid, "no default workspace: give repo as workspace/repo")
		}
		return defaultWorkspace, parts[0], nil
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], nil
	default:
		return "", "", ops.Errorf(ops.Invalid, "invalid repository %q: expected repo or workspace/repo", raw)
	}
}

// Target is a resolved pull request and the repository it lives in. Every
// later call about the PR must use Workspace and Repo from here.
type Target struct {
	Workspace string
	Repo      string
	PR        *bitbucket.PullRequest
}

// Resolve fetches the pull request ref names. A URL decides the repository
// on its own; other selectors use ref.Repo.
func (ref Ref) Resolve(ctx context.Context, env ops.Env) (*Target, error) {
	sel, err := ParseSelector(ref.PR)
	if err != nil {
		return nil, err
	}
	workspace, repo := sel.Workspace, sel.Repo
	if !sel.IsURL() {
		if strings.TrimSpace(ref.Repo) == "" {
			return nil, ops.Errorf(ops.Invalid, "repo is required unless pr is a Bitbucket pull request URL")
		}
		if workspace, repo, err = SplitRepo(ref.Repo, env.DefaultWorkspace()); err != nil {
			return nil, err
		}
	}
	if sel.IsEmpty() {
		if ref.CurrentBranch == "" {
			return nil, ops.Errorf(ops.Invalid, "pr is required: give a PR number, URL, or source branch")
		}
		sel.Branch = ref.CurrentBranch
	}

	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	var pr *bitbucket.PullRequest
	if sel.ID > 0 {
		pr, err = client.GetPullRequest(workspace, repo, sel.ID)
	} else {
		pr, err = client.FindPullRequestByBranch(workspace, repo, sel.Branch)
	}
	if err != nil {
		return nil, err
	}
	return &Target{Workspace: workspace, Repo: repo, PR: pr}, nil
}
