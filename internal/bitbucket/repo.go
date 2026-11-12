package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RepositoryListOptions struct {
	// Name filters with a case-insensitive substring match (q=name ~ "...").
	Name string
	Sort string
	// Limit caps the repositories returned. Zero means MaxListItems.
	Limit int
}

type BranchListOptions struct {
	// Name filters with a case-insensitive substring match (q=name ~ "...").
	Name string
	Sort string
	// Limit caps the branches returned. Zero means MaxListItems.
	Limit int
}

type CommitListOptions struct {
	Include []string
	Exclude []string
	Path    string
	// Limit caps the commits returned. Zero returns only the first page,
	// because history is unbounded.
	Limit int
}

// RepositoryBranch is a branch from the refs API. MergeStrategies and
// DefaultMergeStrategy reflect the repository's merge settings for this branch.
type RepositoryBranch struct {
	Name                 string     `json:"name"`
	Target               CommitInfo `json:"target"`
	MergeStrategies      []string   `json:"merge_strategies,omitempty"`
	DefaultMergeStrategy string     `json:"default_merge_strategy,omitempty"`
	Links                Links      `json:"links"`
}

type CommitInfo struct {
	Hash    string       `json:"hash"`
	Date    time.Time    `json:"date"`
	Message string       `json:"message"`
	Author  CommitAuthor `json:"author"`
	Parents []Commit     `json:"parents,omitempty"`
	Links   Links        `json:"links"`
}

type CommitAuthor struct {
	Raw  string `json:"raw"`
	User *User  `json:"user,omitempty"`
}

func (c *Client) SearchRepositories(ctx context.Context, workspace string, opts *RepositoryListOptions) ([]Repository, error) {
	values := url.Values{}
	limit := 0
	if opts != nil {
		if opts.Name != "" {
			values.Set("q", "name ~ "+bbqlString(opts.Name))
		}
		if opts.Sort != "" {
			values.Set("sort", opts.Sort)
		}
		limit = opts.Limit
	}
	values.Set("pagelen", strconv.Itoa(fullPageLen(limit)))
	path := withQuery("/repositories/"+url.PathEscape(workspace), values)
	repos, _, err := collectPages[Repository](ctx, c, path, limit, "repositories")
	return repos, err
}

func (c *Client) ListBranches(ctx context.Context, workspace, repo string, opts *BranchListOptions) ([]RepositoryBranch, error) {
	values := url.Values{}
	limit := 0
	if opts != nil {
		if opts.Name != "" {
			values.Set("q", "name ~ "+bbqlString(opts.Name))
		}
		if opts.Sort != "" {
			values.Set("sort", opts.Sort)
		}
		limit = opts.Limit
	}
	values.Set("pagelen", strconv.Itoa(fullPageLen(limit)))
	path := withQuery(repoPath(workspace, repo)+"/refs/branches", values)
	branches, _, err := collectPages[RepositoryBranch](ctx, c, path, limit, "branches")
	return branches, err
}

func (c *Client) GetBranch(ctx context.Context, workspace, repo, name string) (*RepositoryBranch, error) {
	escapedName, err := escapeSegments("branch name", strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}

	var branch RepositoryBranch
	path := repoPath(workspace, repo) + "/refs/branches/" + escapedName
	if err := c.getJSON(ctx, path, &branch, "branch"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, NewNotFoundError("branch", name)
		}
		return nil, err
	}
	return &branch, nil
}

// ListCommits lists commits reachable from revision (a branch, tag, or hash)
// and Include, minus those reachable from Exclude, as in "commits on feature
// not on main". A revision containing a slash is sent as an include because
// /commits/{revision} cannot carry one unambiguously.
func (c *Client) ListCommits(ctx context.Context, workspace, repo, revision string, opts *CommitListOptions) ([]CommitInfo, error) {
	path := repoPath(workspace, repo) + "/commits"
	values := url.Values{}
	if strings.Contains(revision, "/") {
		values.Add("include", revision)
	} else if revision != "" {
		path += "/" + url.PathEscape(revision)
	}

	limit := 0
	if opts != nil {
		for _, include := range opts.Include {
			values.Add("include", include)
		}
		for _, exclude := range opts.Exclude {
			values.Add("exclude", exclude)
		}
		if opts.Path != "" {
			values.Set("path", opts.Path)
		}
		limit = opts.Limit
		if pageLen := pageLenFor(limit, 0); pageLen > 0 {
			values.Set("pagelen", strconv.Itoa(pageLen))
		}
	}
	path = withQuery(path, values)

	if limit <= 0 {
		var page PaginatedResponse[CommitInfo]
		if err := c.getJSON(ctx, path, &page, "commits"); err != nil {
			return nil, err
		}
		return page.Values, nil
	}
	commits, _, err := collectPages[CommitInfo](ctx, c, path, limit, "commits")
	return commits, err
}

// GetFileContent returns the raw bytes of path at ref (a branch, tag, or hash).
// A ref containing a slash is resolved to the commit of the branch or tag it
// names first, since /src/{ref}/{path} cannot tell where such a ref ends.
func (c *Client) GetFileContent(ctx context.Context, workspace, repo, ref, path string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("ref cannot be empty")
	}
	escapedPath, err := escapeSegments("file path", path)
	if err != nil {
		return nil, err
	}

	revision := ref
	if strings.Contains(ref, "/") {
		hash, err := c.resolveSlashRef(ctx, workspace, repo, ref)
		if err != nil {
			return nil, err
		}
		revision = hash
	}

	requestURL := c.baseURL + repoPath(workspace, repo) + "/src/" + url.PathEscape(revision) + "/" + escapedPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAuthenticated(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, NewNotFoundError("file", ref+":"+path)
	}
	if err := c.checkResponseWithMessage(resp, body); err != nil {
		return nil, err
	}
	return body, nil
}

type repositoryTag struct {
	Name   string     `json:"name"`
	Target CommitInfo `json:"target"`
}

// resolveSlashRef returns the commit of the branch, else the tag, named ref.
// A ref containing a slash cannot be a commit hash.
func (c *Client) resolveSlashRef(ctx context.Context, workspace, repo, ref string) (string, error) {
	branch, err := c.GetBranch(ctx, workspace, repo, ref)
	if err == nil {
		return refTargetHash("branch", ref, branch.Target.Hash)
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	escapedName, err := escapeSegments("tag name", ref)
	if err != nil {
		return "", err
	}
	var tag repositoryTag
	if err := c.getJSON(ctx, repoPath(workspace, repo)+"/refs/tags/"+escapedName, &tag, "tag"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", NewNotFoundError("branch or tag", ref)
		}
		return "", err
	}
	return refTargetHash("tag", ref, tag.Target.Hash)
}

func refTargetHash(kind, ref, hash string) (string, error) {
	if hash == "" {
		return "", fmt.Errorf("%s %s has no target commit in Bitbucket's response", kind, ref)
	}
	return hash, nil
}
