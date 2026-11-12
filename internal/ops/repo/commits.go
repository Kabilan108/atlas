package repo

import (
	"context"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const defaultCommitLimit = 30

// CommitListInput is the input to commit_list.
type CommitListInput struct {
	ops.RepoRef
	Ref   string `json:"ref,omitempty" jsonschema:"branch, tag, or commit; default: the main branch"`
	Base  string `json:"base,omitempty" jsonschema:"only commits not reachable from this ref, e.g. main"`
	Path  string `json:"path,omitempty" jsonschema:"only commits touching this path"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 30, max 1000"`
}

// CommitListResult is the commits found, newest first.
type CommitListResult struct {
	Repo    string                 `json:"repo"`
	Ref     string                 `json:"ref"`
	Base    string                 `json:"base,omitempty"`
	Commits []bitbucket.CommitInfo `json:"commits"`
	More    bool                   `json:"more,omitempty"`

	limit int
}

var CommitList = &ops.Op[CommitListInput, *CommitListResult]{
	Meta: ops.Meta{
		Name:        "commit_list",
		Description: "List commits on a Bitbucket ref, newest first, or with base only those not on base (what a branch adds).",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      listCommits,
	Markdown: (*CommitListResult).Markdown,
}

func listCommits(ctx context.Context, env ops.Env, in CommitListInput) (*CommitListResult, error) {
	repo, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	limit, err := limitOrDefault(in.Limit, defaultCommitLimit, maxCommitLimit)
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	base := strings.TrimSpace(in.Base)
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	opts := &bitbucket.CommitListOptions{Path: strings.TrimSpace(in.Path), Limit: limit + 1}
	if base != "" {
		opts.Exclude = []string{base}
	}
	commits, err := client.ListCommits(ctx, repo.Workspace, repo.Repo, ref, opts)
	if err != nil {
		return nil, err
	}
	result := &CommitListResult{Repo: repo.String(), Ref: ref, Base: base, limit: limit}
	result.Commits, result.More = trimMore(commits, limit)
	return result, nil
}

// Markdown renders the commits as a table.
func (r *CommitListResult) Markdown() (string, error) {
	scope := r.Ref
	if r.Base != "" {
		scope = r.Base + ".." + r.Ref
	}
	if len(r.Commits) == 0 {
		return "No commits in " + r.Repo + " " + scope + ".\n", nil
	}
	rows := make([][]string, 0, len(r.Commits))
	for _, commit := range r.Commits {
		rows = append(rows, []string{
			shortHash(commit.Hash),
			formatDate(commit),
			authorName(commit.Author),
			output.Truncate(firstLine(commit.Message), 80),
		})
	}
	header := "Commits in " + r.Repo + " " + scope + "\n\n"
	return header + output.MarkdownTable([]string{"Commit", "Date", "Author", "Message"}, rows) + moreNote(r.More, r.limit), nil
}
