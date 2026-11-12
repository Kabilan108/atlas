package pr

import (
	"context"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// CommentInput is the input to pr_comment.
type CommentInput struct {
	Ref
	CommentSpec
	Edit int `json:"edit,omitempty" jsonschema:"ID of a comment to replace with body"`
}

// CommentResult is the comment as Bitbucket stored it. It marshals to the
// JSON `atlas pr comment --json` prints.
type CommentResult struct {
	*bitbucket.Comment

	pullRequest int
	edited      bool
}

// PullRequest is the ID of the pull request the comment is on.
func (r *CommentResult) PullRequest() int { return r.pullRequest }

// Edited reports whether an existing comment was replaced.
func (r *CommentResult) Edited() bool { return r.edited }

var Comment = &ops.Op[CommentInput, *CommentResult]{
	Meta: ops.Meta{
		Name:        "pr_comment",
		Description: "Post one comment on a Bitbucket pull request: top level, a reply, or inline on a file or line. With edit, replace a comment's body. For several comments or a verdict use pr_review.",
		Surfaces:    ops.Both,
	},
	Run: comment,
	Markdown: func(r *CommentResult) (string, error) {
		return compactJSON(struct {
			ID          int    `json:"id"`
			PullRequest int    `json:"pull_request"`
			Edited      bool   `json:"edited,omitempty"`
			URL         string `json:"url,omitempty"`
		}{r.ID, r.pullRequest, r.edited, r.Links.HTML.Href})
	},
}

func comment(ctx context.Context, env ops.Env, in CommentInput) (*CommentResult, error) {
	var create bitbucket.CommentCreate
	if in.Edit != 0 {
		switch {
		case in.Edit < 0:
			return nil, ops.Errorf(ops.Invalid, "edit must be a comment ID")
		case in.ReplyTo != 0 || in.Path != "" || in.Line != 0 || in.Side != "":
			return nil, ops.Errorf(ops.Invalid, "edit replaces only the body; it cannot be combined with reply_to, path, line, or side")
		case strings.TrimSpace(in.Body) == "":
			return nil, ops.Errorf(ops.Invalid, "comment body cannot be empty")
		}
	} else {
		var err error
		if create, err = in.build(); err != nil {
			return nil, err
		}
	}
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	id := target.PR.ID
	var stored *bitbucket.Comment
	if in.Edit != 0 {
		stored, err = client.UpdatePullRequestComment(ctx, target.Workspace, target.Repo, id, in.Edit, in.Body)
	} else {
		stored, err = client.CreatePullRequestComment(ctx, target.Workspace, target.Repo, id, create)
	}
	if err != nil {
		return nil, err
	}
	return &CommentResult{Comment: stored, pullRequest: id, edited: in.Edit != 0}, nil
}

// CommentDeleteInput is the input to pr_comment_delete.
type CommentDeleteInput struct {
	Ref
	Comment int `json:"comment" jsonschema:"ID of the comment to delete"`
}

// CommentDeleteResult confirms a deleted comment.
type CommentDeleteResult struct {
	PullRequest int  `json:"pull_request"`
	Comment     int  `json:"comment"`
	Deleted     bool `json:"deleted"`
}

// CommentDelete is MCP-only as a tool; the CLI reaches it through
// `atlas pr comment --delete`, which is bound to pr_comment.
var CommentDelete = &ops.Op[CommentDeleteInput, *CommentDeleteResult]{
	Meta: ops.Meta{
		Name:        "pr_comment_delete",
		Description: "Delete a comment on a Bitbucket pull request.",
		Surfaces:    ops.MCP,
		Destructive: true,
	},
	Run: deleteComment,
}

func deleteComment(ctx context.Context, env ops.Env, in CommentDeleteInput) (*CommentDeleteResult, error) {
	if in.Comment <= 0 {
		return nil, ops.Errorf(ops.Invalid, "comment is required: give the ID of the comment to delete")
	}
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	if err := client.DeletePullRequestComment(ctx, target.Workspace, target.Repo, target.PR.ID, in.Comment); err != nil {
		return nil, err
	}
	return &CommentDeleteResult{PullRequest: target.PR.ID, Comment: in.Comment, Deleted: true}, nil
}
