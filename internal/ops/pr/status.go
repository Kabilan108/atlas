package pr

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// StatusInput is the input to pr_status.
type StatusInput struct {
	ops.RepoRef
	Branch string `json:"branch,omitempty" jsonschema:"source branch whose PR to show as current"`
}

// StatusResult sorts a repository's open pull requests by how they concern
// the caller. It marshals to the JSON `atlas pr status --json` prints.
type StatusResult struct {
	CurrentBranch   *bitbucket.PullRequest  `json:"currentBranch,omitempty"`
	CreatedByYou    []bitbucket.PullRequest `json:"createdByYou"`
	ReviewRequested []bitbucket.PullRequest `json:"reviewRequested"`

	repo   ops.Repository
	branch string
	user   bitbucket.User
	// more is set when the repo has more open pull requests than were read.
	more bool
}

var Status = &ops.Op[StatusInput, *StatusResult]{
	Meta: ops.Meta{
		Name:        "pr_status",
		Description: "Show the open Bitbucket pull requests in a repo that you created or are asked to review, and the one for branch if given.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      status,
	Markdown: (*StatusResult).Markdown,
}

func status(ctx context.Context, env ops.Env, in StatusInput) (*StatusResult, error) {
	repo, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	listed, err := client.ListPullRequests(ctx, repo.Workspace, repo.Repo, &bitbucket.PRListOptions{States: []string{"OPEN"}, Limit: bitbucket.MaxPRListLimit})
	if err != nil {
		return nil, err
	}
	result := &StatusResult{repo: repo, branch: in.Branch, user: *user, more: listed.More}
	for _, pr := range listed.PullRequests {
		if in.Branch != "" && pr.Source.Branch.Name == in.Branch {
			current := pr
			result.CurrentBranch = &current
		}
		if pr.Author.SharesStableIdentity(*user) {
			result.CreatedByYou = append(result.CreatedByYou, pr)
		}
		if HasReviewer(pr, *user) {
			result.ReviewRequested = append(result.ReviewRequested, pr)
		}
	}
	return result, nil
}

// Markdown renders the three groups as lists.
func (r *StatusResult) Markdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Open pull requests in %s for %s\n", r.repo, r.user.DisplayName)
	if r.branch != "" {
		fmt.Fprintf(&b, "\n## Branch %s\n", r.branch)
		if r.CurrentBranch == nil {
			b.WriteString("- No pull request\n")
		} else {
			writeStatusLine(&b, *r.CurrentBranch)
		}
	}
	for _, group := range []struct {
		title string
		prs   []bitbucket.PullRequest
	}{{"Created by you", r.CreatedByYou}, {"Requesting your review", r.ReviewRequested}} {
		fmt.Fprintf(&b, "\n## %s\n", group.title)
		if len(group.prs) == 0 {
			b.WriteString("- None\n")
		}
		for _, pr := range group.prs {
			writeStatusLine(&b, pr)
		}
	}
	if r.more {
		fmt.Fprintf(&b, "\nOnly the %d most recently updated open pull requests were read.\n", bitbucket.MaxPRListLimit)
	}
	return b.String(), nil
}

func writeStatusLine(b *strings.Builder, pr bitbucket.PullRequest) {
	title := pr.Title
	if pr.Draft {
		title = "[draft] " + title
	}
	fmt.Fprintf(b, "- #%d %s (%s → %s): %s", pr.ID, title, pr.Source.Branch.Name, pr.Destination.Branch.Name, reviewSummary(pr))
	if feedback := FeedbackSummary(pr); feedback != "" {
		fmt.Fprintf(b, "; %s", feedback)
	}
	b.WriteString("\n")
}
