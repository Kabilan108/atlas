package pr

import (
	"context"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// ApproveInput is the input to pr_approve and pr_request_changes.
type ApproveInput struct {
	Ref
	Body string `json:"body,omitempty" jsonschema:"comment to post first"`
}

// ApprovalResult is your participant entry after the action. It marshals to
// the JSON `atlas pr approve --json` prints.
type ApprovalResult struct {
	*bitbucket.ReviewActionResult

	pullRequest int
}

// PullRequest is the ID of the pull request acted on.
func (r *ApprovalResult) PullRequest() int { return r.pullRequest }

func (r *ApprovalResult) receipt() (string, error) {
	return compactJSON(struct {
		PullRequest int    `json:"pull_request"`
		State       string `json:"state"`
		Approved    bool   `json:"approved"`
	}{r.pullRequest, r.State, r.Approved})
}

var Approve = &ops.Op[ApproveInput, *ApprovalResult]{
	Meta: ops.Meta{
		Name:        "pr_approve",
		Description: "Approve a Bitbucket pull request, optionally commenting first.",
		Surfaces:    ops.Both,
	},
	Run:      reviewAction(ReviewApprove),
	Markdown: (*ApprovalResult).receipt,
}

var RequestChanges = &ops.Op[ApproveInput, *ApprovalResult]{
	Meta: ops.Meta{
		Name:        "pr_request_changes",
		Description: "Request changes on a Bitbucket pull request, optionally commenting first.",
		Surfaces:    ops.Both,
	},
	Run:      reviewAction(ReviewRequestChanges),
	Markdown: (*ApprovalResult).receipt,
}

func reviewAction(action ReviewAction) func(context.Context, ops.Env, ApproveInput) (*ApprovalResult, error) {
	return func(ctx context.Context, env ops.Env, in ApproveInput) (*ApprovalResult, error) {
		target, err := in.Resolve(ctx, env)
		if err != nil {
			return nil, err
		}
		client, err := env.Bitbucket(ctx)
		if err != nil {
			return nil, err
		}
		ws, repo, id := target.Workspace, target.Repo, target.PR.ID
		if in.Body != "" {
			if _, err := client.CreatePullRequestCommentText(ctx, ws, repo, id, in.Body); err != nil {
				return nil, err
			}
		}
		var result *bitbucket.ReviewActionResult
		if action == ReviewApprove {
			result, err = client.ApprovePullRequest(ctx, ws, repo, id)
		} else {
			result, err = client.RequestPullRequestChanges(ctx, ws, repo, id)
		}
		if err != nil {
			if in.Body != "" {
				return nil, fmt.Errorf("posted comment but failed to %s pull request: %w", actionPhrase(action), err)
			}
			return nil, err
		}
		return &ApprovalResult{ReviewActionResult: result, pullRequest: id}, nil
	}
}

// UnapproveResult confirms a withdrawn approval. It marshals to the JSON
// `atlas pr unapprove --json` prints.
type UnapproveResult struct {
	Approved    bool `json:"approved"`
	PullRequest int  `json:"pull_request"`
}

// ClearChangeRequestResult confirms a withdrawn change request. It marshals
// to the JSON `atlas pr clear-change-request --json` prints.
type ClearChangeRequestResult struct {
	ChangesRequested bool `json:"changes_requested"`
	PullRequest      int  `json:"pull_request"`
}

var Unapprove = &ops.Op[Ref, *UnapproveResult]{
	Meta: ops.Meta{
		Name:        "pr_unapprove",
		Description: "Withdraw your approval of a Bitbucket pull request.",
		Surfaces:    ops.Both,
	},
	Run: func(ctx context.Context, env ops.Env, in Ref) (*UnapproveResult, error) {
		id, err := withTarget(ctx, env, in, (*bitbucket.Client).UnapprovePullRequest)
		if err != nil {
			return nil, err
		}
		return &UnapproveResult{PullRequest: id}, nil
	},
}

var ClearChangeRequest = &ops.Op[Ref, *ClearChangeRequestResult]{
	Meta: ops.Meta{
		Name:        "pr_clear_change_request",
		Description: "Withdraw your change request on a Bitbucket pull request.",
		Surfaces:    ops.Both,
	},
	Run: func(ctx context.Context, env ops.Env, in Ref) (*ClearChangeRequestResult, error) {
		id, err := withTarget(ctx, env, in, (*bitbucket.Client).ClearPullRequestChanges)
		if err != nil {
			return nil, err
		}
		return &ClearChangeRequestResult{PullRequest: id}, nil
	},
}

// withTarget resolves ref and calls do on the pull request, returning its ID.
func withTarget(ctx context.Context, env ops.Env, ref Ref, do func(*bitbucket.Client, context.Context, string, string, int) error) (int, error) {
	target, err := ref.Resolve(ctx, env)
	if err != nil {
		return 0, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return 0, err
	}
	if err := do(client, ctx, target.Workspace, target.Repo, target.PR.ID); err != nil {
		return 0, err
	}
	return target.PR.ID, nil
}
