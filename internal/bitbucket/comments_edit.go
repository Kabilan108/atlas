package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type commentUpdate struct {
	Content ContentInput `json:"content"`
}

func pullRequestCommentPath(workspace, repo string, id, commentID int) string {
	return fmt.Sprintf("%s/pullrequests/%d/comments/%d", repoPath(workspace, repo), id, commentID)
}

func (c *Client) UpdatePullRequestComment(workspace, repo string, id, commentID int, body string) (*Comment, error) {
	_, data, err := c.send(http.MethodPut, pullRequestCommentPath(workspace, repo, id, commentID), commentUpdate{
		Content: ContentInput{Raw: body},
	})
	if err != nil {
		return nil, err
	}
	c.invalidatePRCache()

	var comment Comment
	if err := json.Unmarshal(data, &comment); err != nil {
		return nil, fmt.Errorf("failed to parse comment response: %w", err)
	}
	return &comment, nil
}

func (c *Client) DeletePullRequestComment(workspace, repo string, id, commentID int) error {
	if _, _, err := c.send(http.MethodDelete, pullRequestCommentPath(workspace, repo, id, commentID), nil); err != nil {
		return err
	}
	c.invalidatePRCache()
	return nil
}
