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
	ops.RepoRef
	PR string `json:"pr" jsonschema:"PR number, source branch name, or Bitbucket PR URL (which needs no repo)"`

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
	return s.ID == 0 && s.Branch == ""
}

// ParseSelector accepts a PR number (optionally prefixed with #), a
// bitbucket.org pull request URL, or a branch name. A URL's workspace and
// repo are validated like a repo input.
func ParseSelector(raw string) (Selector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Selector{}, nil
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host == "bitbucket.org" {
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull-requests" {
			return urlSelector(raw, parts)
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

func urlSelector(raw string, parts []string) (Selector, error) {
	id, err := strconv.Atoi(parts[3])
	if err != nil || id <= 0 {
		return Selector{}, ops.Errorf(ops.Invalid, "invalid pull request URL %q", raw)
	}
	names := make([]string, 2)
	for i, kind := range []string{"workspace", "repository"} {
		name, err := url.PathUnescape(parts[i])
		if err != nil {
			return Selector{}, ops.Errorf(ops.Invalid, "invalid pull request URL %q: %w", raw, err)
		}
		if err := ops.ValidateSlug(kind, name); err != nil {
			return Selector{}, ops.Errorf(ops.Invalid, "invalid pull request URL %q: %w", raw, err)
		}
		names[i] = name
	}
	return Selector{Workspace: names[0], Repo: names[1], ID: id}, nil
}

// Target is a resolved pull request and the repository it lives in. Every
// later call about the PR must use Workspace and Repo from here.
type Target struct {
	ops.Repository
	PR *bitbucket.PullRequest
}

// Resolve fetches the pull request ref names. A URL decides the repository
// on its own; other selectors use ref.Repo.
func (ref Ref) Resolve(ctx context.Context, env ops.Env) (*Target, error) {
	sel, err := ParseSelector(ref.PR)
	if err != nil {
		return nil, err
	}
	repo := ops.Repository{Workspace: sel.Workspace, Repo: sel.Repo}
	if !sel.IsURL() {
		if strings.TrimSpace(ref.Repo) == "" {
			return nil, ops.Errorf(ops.Invalid, "repo is required unless pr is a Bitbucket pull request URL")
		}
		if repo, err = ref.RepoRef.Resolve(env); err != nil {
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
		pr, err = client.GetPullRequest(ctx, repo.Workspace, repo.Repo, sel.ID)
	} else {
		pr, err = client.FindPullRequestByBranch(ctx, repo.Workspace, repo.Repo, sel.Branch)
	}
	if err != nil {
		return nil, err
	}
	return &Target{Repository: repo, PR: pr}, nil
}
