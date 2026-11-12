package pr

import (
	"context"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// CloseInput is the input to pr_close.
type CloseInput struct {
	Ref
	Comment string `json:"comment,omitempty" jsonschema:"comment to post before declining"`
}

var Close = &ops.Op[CloseInput, *bitbucket.PullRequest]{
	Meta: ops.Meta{
		Name:        "pr_close",
		Description: "Decline (close without merging) a Bitbucket pull request, optionally posting a comment first.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run:      closePR,
	Markdown: func(pr *bitbucket.PullRequest) (string, error) { return compactJSON(receiptFor(pr)) },
}

func closePR(ctx context.Context, env ops.Env, in CloseInput) (*bitbucket.PullRequest, error) {
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	if in.Comment != "" {
		if _, err := client.CreatePullRequestCommentText(ctx, target.Workspace, target.Repo, target.PR.ID, in.Comment); err != nil {
			return nil, err
		}
	}
	closed, err := client.DeclinePullRequest(ctx, target.Workspace, target.Repo, target.PR.ID)
	if err != nil {
		if in.Comment != "" {
			return nil, fmt.Errorf("posted closing comment but failed to close pull request: %w", err)
		}
		return nil, err
	}
	return closed, nil
}
