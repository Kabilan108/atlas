package pr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// ReviewCounts counts pr's reviewers by where their review stands.
func ReviewCounts(pr bitbucket.PullRequest) (approved, changes, pending int) {
	for _, reviewer := range pr.Reviewers {
		pending++
		for _, participant := range pr.Participants {
			if participant.Role != "REVIEWER" || !participant.User.SharesStableIdentity(reviewer) {
				continue
			}
			pending--
			switch {
			case participant.Approved:
				approved++
			case participant.State == "changes_requested":
				changes++
			default:
				pending++
			}
			break
		}
	}
	return approved, changes, pending
}

// reviewSummary names pr's reviewers with their verdicts, approvals first,
// such as "Ada approved, Linus requested changes, Grace pending", so an
// audit needs no per-PR view. Someone who approved or requested changes
// without being a reviewer is named too.
func reviewSummary(pr bitbucket.PullRequest) string {
	var approved, changes, pending []string
	verdict := func(p bitbucket.Participant) *[]string {
		switch {
		case p.Approved:
			return &approved
		case p.State == "changes_requested":
			return &changes
		}
		return nil
	}
	for _, reviewer := range pr.Reviewers {
		group := &pending
		for _, participant := range pr.Participants {
			if participant.User.SharesStableIdentity(reviewer) {
				if g := verdict(participant); g != nil {
					group = g
				}
				break
			}
		}
		*group = append(*group, reviewerName(reviewer))
	}
	for _, participant := range pr.Participants {
		if isReviewer(pr, participant.User) {
			continue
		}
		if group := verdict(participant); group != nil {
			*group = append(*group, reviewerName(participant.User))
		}
	}
	var parts []string
	for _, name := range approved {
		parts = append(parts, name+" approved")
	}
	for _, name := range changes {
		parts = append(parts, name+" requested changes")
	}
	for _, name := range pending {
		parts = append(parts, name+" pending")
	}
	if len(parts) == 0 {
		return "no reviewers"
	}
	return strings.Join(parts, ", ")
}

func isReviewer(pr bitbucket.PullRequest, user bitbucket.User) bool {
	for _, reviewer := range pr.Reviewers {
		if reviewer.SharesStableIdentity(user) {
			return true
		}
	}
	return false
}

func reviewerName(user bitbucket.User) string {
	for _, name := range []string{user.DisplayName, user.Nickname, user.Username, user.AccountID} {
		if name != "" {
			return name
		}
	}
	return "unknown user"
}

// FeedbackSummary is pr's comment and task counts, such as "5 comments, 2
// tasks", or "" when it has neither.
func FeedbackSummary(pr bitbucket.PullRequest) string {
	var parts []string
	if pr.CommentCount > 0 {
		parts = append(parts, fmt.Sprintf("%d comments", pr.CommentCount))
	}
	if pr.TaskCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tasks", pr.TaskCount))
	}
	return strings.Join(parts, ", ")
}

// HasReviewer reports whether user reviews pr.
func HasReviewer(pr bitbucket.PullRequest, user bitbucket.User) bool {
	for _, reviewer := range pr.Reviewers {
		if reviewer.SharesStableIdentity(user) {
			return true
		}
	}
	for _, participant := range pr.Participants {
		if participant.Role == "REVIEWER" && participant.User.SharesStableIdentity(user) {
			return true
		}
	}
	return false
}

// prReceipt is what write ops tell agents about a pull request.
type prReceipt struct {
	ID          int    `json:"id"`
	Title       string `json:"title,omitempty"`
	State       string `json:"state,omitempty"`
	Draft       bool   `json:"draft,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	// SourceCommit and DestinationCommit are the branch heads Bitbucket
	// reports after the write, so a retarget or create needs no readback.
	SourceCommit      string `json:"source_commit,omitempty"`
	DestinationCommit string `json:"destination_commit,omitempty"`
	URL               string `json:"url,omitempty"`
}

func receiptFor(pr *bitbucket.PullRequest) prReceipt {
	return prReceipt{
		ID:                pr.ID,
		Title:             pr.Title,
		State:             pr.State,
		Draft:             pr.Draft,
		Source:            pr.Source.Branch.Name,
		Destination:       pr.Destination.Branch.Name,
		SourceCommit:      shortHash(pr.Source.Commit.Hash),
		DestinationCommit: shortHash(pr.Destination.Commit.Hash),
		URL:               pr.Links.HTML.Href,
	}
}

func compactJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

// tableCell keeps text on one line of a markdown table row.
func tableCell(text string) string {
	text = strings.ReplaceAll(text, "|", `\|`)
	return strings.Join(strings.Fields(text), " ")
}

const timeLayout = "2006-01-02 15:04 UTC"
