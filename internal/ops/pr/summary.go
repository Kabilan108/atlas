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

// reviewSummary is ReviewCounts as text, such as "1 approved, 2 pending".
func reviewSummary(pr bitbucket.PullRequest) string {
	approved, changes, pending := ReviewCounts(pr)
	var parts []string
	if approved > 0 {
		parts = append(parts, fmt.Sprintf("%d approved", approved))
	}
	if changes > 0 {
		parts = append(parts, fmt.Sprintf("%d changes requested", changes))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if len(parts) == 0 {
		return "no reviewers"
	}
	return strings.Join(parts, ", ")
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
	URL         string `json:"url,omitempty"`
}

func receiptFor(pr *bitbucket.PullRequest) prReceipt {
	return prReceipt{
		ID:          pr.ID,
		Title:       pr.Title,
		State:       pr.State,
		Draft:       pr.Draft,
		Source:      pr.Source.Branch.Name,
		Destination: pr.Destination.Branch.Name,
		URL:         pr.Links.HTML.Href,
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
