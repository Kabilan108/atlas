package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	ActivityKindApproval         = "approval"
	ActivityKindChangesRequested = "changes_requested"
	ActivityKindUpdate           = "update"
	ActivityKindComment          = "comment"
	ActivityKindUnknown          = "unknown"
)

// PullRequestActivity is one entry of the PR activity feed. Bitbucket sets
// exactly one of the typed fields; Raw keeps the entry for kinds not modelled here.
type PullRequestActivity struct {
	Approval         *ActivityParticipant `json:"approval,omitempty"`
	ChangesRequested *ActivityParticipant `json:"changes_requested,omitempty"`
	Update           *ActivityUpdate      `json:"update,omitempty"`
	Comment          *Comment             `json:"comment,omitempty"`
	Raw              json.RawMessage      `json:"-"`
}

type ActivityParticipant struct {
	Date time.Time `json:"date"`
	User User      `json:"user"`
}

type ActivityUpdate struct {
	State       string                     `json:"state"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	Reason      string                     `json:"reason"`
	Date        time.Time                  `json:"date"`
	Author      User                       `json:"author"`
	Source      PullRequestRef             `json:"source"`
	Destination PullRequestRef             `json:"destination"`
	Changes     map[string]json.RawMessage `json:"changes,omitempty"`
}

func (a *PullRequestActivity) UnmarshalJSON(data []byte) error {
	type plain PullRequestActivity
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*a = PullRequestActivity(decoded)
	a.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (a PullRequestActivity) Kind() string {
	switch {
	case a.Approval != nil:
		return ActivityKindApproval
	case a.ChangesRequested != nil:
		return ActivityKindChangesRequested
	case a.Update != nil:
		return ActivityKindUpdate
	case a.Comment != nil:
		return ActivityKindComment
	default:
		return ActivityKindUnknown
	}
}

func (a PullRequestActivity) Time() time.Time {
	switch {
	case a.Approval != nil:
		return a.Approval.Date
	case a.ChangesRequested != nil:
		return a.ChangesRequested.Date
	case a.Update != nil:
		return a.Update.Date
	case a.Comment != nil:
		return a.Comment.CreatedOn
	default:
		return time.Time{}
	}
}

func (a PullRequestActivity) Actor() User {
	switch {
	case a.Approval != nil:
		return a.Approval.User
	case a.ChangesRequested != nil:
		return a.ChangesRequested.User
	case a.Update != nil:
		return a.Update.Author
	case a.Comment != nil:
		return a.Comment.User
	default:
		return User{}
	}
}

// ListPullRequestActivity returns the activity feed newest first, as Bitbucket
// orders it. A limit of zero or less means MaxListItems.
func (c *Client) ListPullRequestActivity(ctx context.Context, workspace, repo string, id, limit int) ([]PullRequestActivity, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/activity", repoPath(workspace, repo), id)
	if pageLen := pageLenFor(limit, 0); pageLen > 0 {
		path += fmt.Sprintf("?pagelen=%d", pageLen)
	}
	activity, _, err := collectPages[PullRequestActivity](ctx, c, path, limit, "pull request activity")
	return activity, err
}
