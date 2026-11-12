package pr

import (
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// PostedComment names a comment a write posted.
type PostedComment struct {
	ID  int    `json:"id"`
	URL string `json:"url,omitempty"`
}

// FailedStep reports a comment-then-action write whose comment posted but
// whose action failed. Retry the action without the comment, or the comment
// is posted twice.
type FailedStep struct {
	PullRequest int           `json:"pull_request"`
	Step        string        `json:"step"`
	Comment     PostedComment `json:"comment"`
	Error       string        `json:"error"`
}

// stepFailed builds the FailedStep and the Partial error for an action that
// failed with err after comment posted. message is the error's text before
// err.
func stepFailed(pullRequest int, step string, comment *bitbucket.Comment, err error, message string) (*FailedStep, error) {
	failed := &FailedStep{
		PullRequest: pullRequest,
		Step:        step,
		Comment:     PostedComment{ID: comment.ID, URL: comment.Links.HTML.Href},
		Error:       err.Error(),
	}
	return failed, ops.Errorf(ops.Partial, "%s: %w", message, err)
}

func (f *FailedStep) String() string {
	posted := fmt.Sprintf("Posted comment #%d on pull request #%d", f.Comment.ID, f.PullRequest)
	if f.Comment.URL != "" {
		posted += " (" + f.Comment.URL + ")"
	}
	return fmt.Sprintf("%s, but %s failed; retry without the comment to avoid posting it twice", posted, f.Step)
}
