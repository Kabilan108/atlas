package pr

import (
	"context"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// CreateInput is the input to pr_create.
type CreateInput struct {
	ops.RepoRef
	Title     string   `json:"title"`
	Head      string   `json:"head" jsonschema:"source branch; must already be pushed"`
	Base      string   `json:"base,omitempty" jsonschema:"destination branch; default the repo's main branch"`
	Body      string   `json:"body,omitempty" jsonschema:"markdown description"`
	Reviewers []string `json:"reviewers,omitempty" jsonschema:"account IDs, UUIDs, or exact display names; an ambiguous name fails with the candidates"`
	Draft     bool     `json:"draft,omitempty" jsonschema:"open as a draft, not yet ready for review"`
}

var Create = &ops.Op[CreateInput, *bitbucket.PullRequest]{
	Meta: ops.Meta{
		Name:        "pr_create",
		Description: "Open a Bitbucket pull request from a pushed branch. Returns its ID and URL.",
		Surfaces:    ops.Both,
	},
	Run:      create,
	Markdown: func(pr *bitbucket.PullRequest) (string, error) { return compactJSON(receiptFor(pr)) },
}

func create(ctx context.Context, env ops.Env, in CreateInput) (*bitbucket.PullRequest, error) {
	repo, payload, err := PrepareCreate(ctx, env, in)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	return client.CreatePullRequest(ctx, repo.Workspace, repo.Repo, payload)
}

// PrepareCreate checks in and builds the request pr_create would send,
// looking up the default base branch and the reviewers. It creates nothing,
// so the CLI's --dry-run prints its payload.
func PrepareCreate(ctx context.Context, env ops.Env, in CreateInput) (ops.Repository, bitbucket.PullRequestCreate, error) {
	var payload bitbucket.PullRequestCreate
	repo, err := in.Resolve(env)
	if err != nil {
		return repo, payload, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return repo, payload, ops.Errorf(ops.Invalid, "title is required")
	}
	if strings.TrimSpace(in.Head) == "" {
		return repo, payload, ops.Errorf(ops.Invalid, "head is required: give the pushed source branch")
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return repo, payload, err
	}
	base := in.Base
	if base == "" {
		if base, err = DefaultBase(ctx, client, repo); err != nil {
			return repo, payload, err
		}
	}
	reviewers, err := resolveReviewers(ctx, client, repo.Workspace, in.Reviewers)
	if err != nil {
		return repo, payload, err
	}
	payload = bitbucket.PullRequestCreate{
		Draft:       in.Draft,
		Title:       in.Title,
		Description: NormalizeDescription(in.Body),
		Source:      bitbucket.PullRequestRefInput{Branch: bitbucket.Branch{Name: in.Head}},
		Destination: bitbucket.PullRequestRefInput{Branch: bitbucket.Branch{Name: base}},
		Reviewers:   reviewers,
	}
	return repo, payload, nil
}

// DefaultBase is the branch a new pull request in repo targets when none is
// given: the repository's main branch, or "main".
func DefaultBase(ctx context.Context, client *bitbucket.Client, repo ops.Repository) (string, error) {
	repository, err := client.GetRepository(ctx, repo.Workspace, repo.Repo)
	if err != nil {
		return "", err
	}
	if repository.MainBranch.Name != "" {
		return repository.MainBranch.Name, nil
	}
	return "main", nil
}
