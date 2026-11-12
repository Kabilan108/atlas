package pr

import (
	"context"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// CloseInput is the input to pr_close.
type CloseInput struct {
	Ref
	Body string `json:"body,omitempty" jsonschema:"comment to post before declining"`
}

// CloseResult is the declined pull request. It marshals to the JSON `atlas
// pr close --json` prints. When the decline fails after the comment posted,
// PullRequest is the still-open pull request and Failed says what was posted.
type CloseResult struct {
	*bitbucket.PullRequest
	Failed *FailedStep `json:"failed,omitempty"`
}

func (r *CloseResult) receipt() (string, error) {
	return compactJSON(struct {
		prReceipt
		Failed *FailedStep `json:"failed,omitempty"`
	}{receiptFor(r.PullRequest), r.Failed})
}

var Close = &ops.Op[CloseInput, *CloseResult]{
	Meta: ops.Meta{
		Name:        "pr_close",
		Description: "Decline (close without merging) a Bitbucket pull request, optionally posting a comment first.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run:      closePR,
	Markdown: (*CloseResult).receipt,
}

func closePR(ctx context.Context, env ops.Env, in CloseInput) (*CloseResult, error) {
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	var comment *bitbucket.Comment
	if in.Body != "" {
		if comment, err = client.CreatePullRequestCommentText(ctx, target.Workspace, target.Repo, target.PR.ID, in.Body); err != nil {
			return nil, err
		}
	}
	closed, err := client.DeclinePullRequest(ctx, target.Workspace, target.Repo, target.PR.ID)
	if err != nil {
		if comment == nil {
			return nil, err
		}
		failed, err := stepFailed(target.PR.ID, "decline", comment, err, "posted closing comment but failed to close pull request")
		return &CloseResult{PullRequest: target.PR, Failed: failed}, err
	}
	return &CloseResult{PullRequest: closed}, nil
}
