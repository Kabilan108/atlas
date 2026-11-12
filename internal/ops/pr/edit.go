package pr

import (
	"context"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// EditInput is the input to pr_edit. Fields left out stay as they are.
type EditInput struct {
	Ref
	Title           string   `json:"title,omitempty"`
	Body            *string  `json:"body,omitempty" jsonschema:"new markdown description; empty clears it"`
	Base            string   `json:"base,omitempty" jsonschema:"retarget to this destination branch"`
	Draft           *bool    `json:"draft,omitempty" jsonschema:"true marks draft, false ready for review"`
	AddReviewers    []string `json:"add_reviewers,omitempty" jsonschema:"account IDs, UUIDs, or exact display names; an ambiguous name fails with the candidates"`
	RemoveReviewers []string `json:"remove_reviewers,omitempty" jsonschema:"account IDs, UUIDs, or exact display names; an ambiguous name fails with the candidates"`
}

// EditResult is the pull request after the edit. Changed is false when
// nothing differed, in which case nothing was sent.
type EditResult struct {
	*bitbucket.PullRequest
	Changed bool `json:"changed"`
}

var Edit = &ops.Op[EditInput, *EditResult]{
	Meta: ops.Meta{
		Name:        "pr_edit",
		Description: "Edit a Bitbucket pull request: title, description, destination branch, draft state, or reviewers.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run: edit,
	Markdown: func(r *EditResult) (string, error) {
		return compactJSON(struct {
			prReceipt
			Changed bool `json:"changed"`
		}{receiptFor(r.PullRequest), r.Changed})
	},
}

func edit(ctx context.Context, env ops.Env, in EditInput) (*EditResult, error) {
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	current := target.PR
	var update bitbucket.PullRequestUpdate
	changed := false
	if in.Draft != nil {
		draft := *in.Draft
		update.Draft, changed = &draft, true
	}
	if in.Title != "" && in.Title != current.Title {
		title := in.Title
		update.Title, changed = &title, true
	}
	if in.Body != nil {
		if body := NormalizeDescription(*in.Body); body != current.Description {
			update.Description, changed = &body, true
		}
	}
	if in.Base != "" && in.Base != current.Destination.Branch.Name {
		update.Destination = &bitbucket.PullRequestRefInput{Branch: bitbucket.Branch{Name: in.Base}}
		changed = true
	}
	if len(in.AddReviewers) > 0 || len(in.RemoveReviewers) > 0 {
		reviewers, err := editReviewers(ctx, client, target.Workspace, current.Reviewers, in.AddReviewers, in.RemoveReviewers)
		if err != nil {
			return nil, err
		}
		update.Reviewers, changed = &reviewers, true
	}
	if !changed {
		return &EditResult{PullRequest: current}, nil
	}
	updated, err := client.UpdatePullRequest(ctx, target.Workspace, target.Repo, current.ID, update)
	if err != nil {
		return nil, err
	}
	return &EditResult{PullRequest: updated, Changed: true}, nil
}

func editReviewers(ctx context.Context, client *bitbucket.Client, workspace string, current []bitbucket.User, addIDs, removeIDs []string) ([]bitbucket.User, error) {
	removed := make([]bool, len(current))
	for _, input := range removeIDs {
		who := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "@"))
		if isUserID(who) {
			for i, reviewer := range current {
				if reviewer.MatchesStableIdentifier(who) {
					removed[i] = true
				}
			}
			continue
		}
		match, err := matchName(current, who, "reviewer", "this pull request")
		if err != nil {
			return nil, err
		}
		for i, reviewer := range current {
			if reviewer.SharesStableIdentity(match) {
				removed[i] = true
			}
		}
	}
	reviewers := make([]bitbucket.User, 0, len(current)+len(addIDs))
	for i, reviewer := range current {
		if !removed[i] {
			reviewers = append(reviewers, reviewer)
		}
	}
	added, err := resolveReviewers(ctx, client, workspace, addIDs)
	if err != nil {
		return nil, err
	}
	for _, reviewer := range added {
		exists := false
		for _, existing := range reviewers {
			if existing.SharesStableIdentity(reviewer) {
				exists = true
				break
			}
		}
		if !exists {
			reviewers = append(reviewers, reviewer)
		}
	}
	return reviewers, nil
}
