package pr

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// ReviewAction is how a review finishes.
type ReviewAction string

const (
	ReviewApprove        ReviewAction = "approve"
	ReviewRequestChanges ReviewAction = "request-changes"
	ReviewComment        ReviewAction = "comment"
)

func (ReviewAction) Enum() []string {
	return []string{string(ReviewApprove), string(ReviewRequestChanges), string(ReviewComment)}
}

// ReviewInput is the input to pr_review.
type ReviewInput struct {
	Ref
	Action   ReviewAction  `json:"action"`
	Body     string        `json:"body,omitempty" jsonschema:"summary comment, posted after comments"`
	Comments []CommentSpec `json:"comments,omitempty" jsonschema:"posted in order before the summary; all are checked before any is posted"`
}

// EntryResult is the outcome of one entry of a batch: ok, failed, or
// skipped because an earlier entry failed.
type EntryResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	ID     int    `json:"id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ReviewResult is what a review posted. It marshals to the JSON `atlas pr
// review --json` prints; Results appears only when a step failed.
type ReviewResult struct {
	PullRequest int                           `json:"pull_request"`
	Action      ReviewAction                  `json:"action"`
	Comments    []bitbucket.Comment           `json:"comments,omitempty"`
	Result      *bitbucket.ReviewActionResult `json:"result,omitempty"`
	Results     []EntryResult                 `json:"results,omitempty"`

	results  []EntryResult
	summary  int
	complete bool
}

// Complete reports whether every step of the review succeeded.
func (r *ReviewResult) Complete() bool { return r.complete }

var Review = &ops.Op[ReviewInput, *ReviewResult]{
	Meta: ops.Meta{
		Name:        "pr_review",
		Description: "Review a Bitbucket pull request in one call: post inline and reply comments, a summary, then approve, request changes, or leave it at comments.",
		Surfaces:    ops.Both,
	},
	Run:      review,
	Markdown: (*ReviewResult).receipt,
}

func review(ctx context.Context, env ops.Env, in ReviewInput) (*ReviewResult, error) {
	if in.Action == "" {
		return nil, ops.Errorf(ops.Invalid, "action is required: approve, request-changes, or comment")
	}
	hasSummary := strings.TrimSpace(in.Body) != ""
	if in.Action == ReviewComment && len(in.Comments) == 0 && !hasSummary {
		return nil, ops.Errorf(ops.Invalid, "a comment-only review needs body or comments")
	}
	creates := make([]bitbucket.CommentCreate, len(in.Comments))
	for i, spec := range in.Comments {
		create, err := spec.build()
		if err != nil {
			return nil, ops.Errorf(ops.Invalid, "comments[%d]: %w", i, err)
		}
		creates[i] = create
	}
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	ws, repo, id := target.Workspace, target.Repo, target.PR.ID

	result := &ReviewResult{PullRequest: id, Action: in.Action}
	for i, create := range creates {
		comment, err := client.CreatePullRequestComment(ctx, ws, repo, id, create)
		if err != nil {
			result.results = append(result.results, EntryResult{Index: i, Status: "failed", Error: err.Error()})
			for skipped := i + 1; skipped < len(creates); skipped++ {
				result.results = append(result.results, EntryResult{Index: skipped, Status: "skipped"})
			}
			result.Results = result.results
			cause := fmt.Errorf("posted %d comments but failed to post comment %d: %w", len(result.Comments), i+1, err)
			return result, errors.Join(ops.PartialOf(1, len(creates), "comments"), cause)
		}
		result.Comments = append(result.Comments, *comment)
		result.results = append(result.results, EntryResult{Index: i, Status: "ok", ID: comment.ID})
	}
	if hasSummary {
		comment, err := client.CreatePullRequestCommentText(ctx, ws, repo, id, in.Body)
		if err != nil {
			return result.failed(fmt.Errorf("posted %d comments but failed to post review summary: %w", len(result.Comments), err))
		}
		result.Comments = append(result.Comments, *comment)
		result.summary = comment.ID
	}
	switch in.Action {
	case ReviewApprove:
		result.Result, err = client.ApprovePullRequest(ctx, ws, repo, id)
	case ReviewRequestChanges:
		result.Result, err = client.RequestPullRequestChanges(ctx, ws, repo, id)
	}
	if err != nil {
		return result.failed(fmt.Errorf("posted %d comments but failed to %s pull request: %w", len(result.Comments), actionPhrase(in.Action), err))
	}
	result.complete = true
	return result, nil
}

// failed ends a review whose comments all posted but whose summary or
// action did not: partial when something was posted, a plain error if not.
func (r *ReviewResult) failed(err error) (*ReviewResult, error) {
	if len(r.Comments) == 0 {
		return nil, err
	}
	r.Results = r.results
	return r, ops.Errorf(ops.Partial, "%w", err)
}

func actionPhrase(action ReviewAction) string {
	if action == ReviewRequestChanges {
		return "request changes on"
	}
	return string(action)
}

func (r *ReviewResult) receipt() (string, error) {
	state := ""
	if r.Result != nil {
		state = r.Result.State
	}
	return compactJSON(struct {
		PullRequest int           `json:"pull_request"`
		Action      ReviewAction  `json:"action"`
		Done        bool          `json:"done"`
		State       string        `json:"state,omitempty"`
		Summary     int           `json:"summary_comment,omitempty"`
		Results     []EntryResult `json:"results,omitempty"`
	}{r.PullRequest, r.Action, r.complete, state, r.summary, r.results})
}
