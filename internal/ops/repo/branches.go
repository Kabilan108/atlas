package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const defaultBranchLimit = 30

// BranchListInput is the input to branch_list.
type BranchListInput struct {
	ops.RepoRef
	Query string `json:"query,omitempty" jsonschema:"case-insensitive substring of the branch name"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 30, max 1000"`
}

// BranchListResult is the branches found, most recent commit first.
type BranchListResult struct {
	Repo     string                       `json:"repo"`
	Branches []bitbucket.RepositoryBranch `json:"branches"`
	More     bool                         `json:"more,omitempty"`

	limit int
}

var BranchList = &ops.Op[BranchListInput, *BranchListResult]{
	Meta: ops.Meta{
		Name:        "branch_list",
		Description: "List a Bitbucket repo's branches, most recent commit first, with head commit, date, and author.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      listBranches,
	Markdown: (*BranchListResult).Markdown,
}

func listBranches(ctx context.Context, env ops.Env, in BranchListInput) (*BranchListResult, error) {
	repo, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	limit, err := limitOrDefault(in.Limit, defaultBranchLimit, maxBranchLimit)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	branches, err := client.ListBranches(ctx, repo.Workspace, repo.Repo, &bitbucket.BranchListOptions{
		Name:  strings.TrimSpace(in.Query),
		Sort:  "-target.date",
		Limit: limit + 1,
	})
	if err != nil {
		return nil, err
	}
	result := &BranchListResult{Repo: repo.String(), limit: limit}
	result.Branches, result.More = trimMore(branches, limit)
	return result, nil
}

// Markdown renders the branches as a table.
func (r *BranchListResult) Markdown() (string, error) {
	if len(r.Branches) == 0 {
		return "No branches found in " + r.Repo + ".\n", nil
	}
	rows := make([][]string, 0, len(r.Branches))
	for _, branch := range r.Branches {
		rows = append(rows, []string{
			branch.Name,
			shortHash(branch.Target.Hash),
			formatDate(branch.Target),
			authorName(branch.Target.Author),
			output.Truncate(firstLine(branch.Target.Message), 72),
		})
	}
	return output.MarkdownTable([]string{"Branch", "Head", "Date", "Author", "Message"}, rows) + moreNote(r.More, r.limit), nil
}

// BranchViewInput is the input to branch_view.
type BranchViewInput struct {
	ops.RepoRef
	Branch string `json:"branch" jsonschema:"branch name"`
}

// BranchViewResult says whether a branch exists and, if so, its head commit
// and merge settings.
type BranchViewResult struct {
	Repo   string                      `json:"repo"`
	Name   string                      `json:"name"`
	Exists bool                        `json:"exists"`
	Branch *bitbucket.RepositoryBranch `json:"branch,omitempty"`
}

var BranchView = &ops.Op[BranchViewInput, *BranchViewResult]{
	Meta: ops.Meta{
		Name:        "branch_view",
		Description: "Check whether a branch exists in a Bitbucket repo; if so show its head commit and default merge strategy.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      viewBranch,
	Markdown: (*BranchViewResult).Markdown,
}

func viewBranch(ctx context.Context, env ops.Env, in BranchViewInput) (*BranchViewResult, error) {
	repo, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Branch)
	if name == "" {
		return nil, ops.Errorf(ops.Invalid, "branch is required")
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	result := &BranchViewResult{Repo: repo.String(), Name: name}
	branch, err := client.GetBranch(ctx, repo.Workspace, repo.Repo, name)
	if errors.Is(err, bitbucket.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Exists, result.Branch = true, branch
	return result, nil
}

// Markdown renders the branch summary.
func (r *BranchViewResult) Markdown() (string, error) {
	if !r.Exists {
		return fmt.Sprintf("Branch %s does not exist in %s.\n", r.Name, r.Repo), nil
	}
	var b strings.Builder
	target := r.Branch.Target
	fmt.Fprintf(&b, "Branch %s in %s\n\n", r.Branch.Name, r.Repo)
	fmt.Fprintf(&b, "- Head: %s (%s, %s)\n", target.Hash, formatDate(target), authorName(target.Author))
	fmt.Fprintf(&b, "- Message: %s\n", firstLine(target.Message))
	if r.Branch.DefaultMergeStrategy != "" {
		fmt.Fprintf(&b, "- Default merge strategy: %s\n", r.Branch.DefaultMergeStrategy)
	}
	if len(r.Branch.MergeStrategies) > 0 {
		fmt.Fprintf(&b, "- Allowed merge strategies: %s\n", strings.Join(r.Branch.MergeStrategies, ", "))
	}
	return b.String(), nil
}

func formatDate(commit bitbucket.CommitInfo) string {
	if commit.Date.IsZero() {
		return ""
	}
	return commit.Date.UTC().Format("2006-01-02 15:04")
}
