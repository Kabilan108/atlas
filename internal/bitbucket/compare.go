package bitbucket

import (
	"context"
	"net/url"
	"strings"
)

// DiffStat is one file's entry in a diffstat. Old is nil for an added file
// and New for a removed one.
type DiffStat struct {
	Status       string      `json:"status"`
	LinesAdded   int         `json:"lines_added"`
	LinesRemoved int         `json:"lines_removed"`
	Old          *CommitFile `json:"old"`
	New          *CommitFile `json:"new"`
}

// CommitFile is a file at one side of a diffstat entry.
type CommitFile struct {
	Path string `json:"path"`
}

// CompareSpec is the escaped {spec} path segment for the changes head makes
// relative to base. Bitbucket names the head first, the reverse of git diff.
// A ref containing a slash is resolved to its commit, as for file reads.
func (c *Client) CompareSpec(ctx context.Context, workspace, repo, base, head string) (string, error) {
	revisions := [2]string{head, base}
	for i, ref := range revisions {
		if strings.Contains(ref, "/") {
			hash, err := c.resolveSlashRef(ctx, workspace, repo, ref)
			if err != nil {
				return "", err
			}
			revisions[i] = hash
		}
	}
	return url.PathEscape(revisions[0]) + ".." + url.PathEscape(revisions[1]), nil
}

// topic asks for the three-dot form: head against its merge base with base,
// which is what a pull request shows.
const topic = "?topic=true"

// GetCompareDiffLimit reads at most maxBytes of the diff for a CompareSpec.
func (c *Client) GetCompareDiffLimit(ctx context.Context, workspace, repo, spec string, maxBytes int64) (*LimitedBody, error) {
	return c.getLimited(ctx, repoPath(workspace, repo)+"/diff/"+spec+topic, maxBytes, false)
}

// ListCompareDiffStat returns up to limit diffstat entries for a CompareSpec.
func (c *Client) ListCompareDiffStat(ctx context.Context, workspace, repo, spec string, limit int) ([]DiffStat, Scan, error) {
	return collectPages[DiffStat](ctx, c, repoPath(workspace, repo)+"/diffstat/"+spec+topic, limit, "diffstat")
}
