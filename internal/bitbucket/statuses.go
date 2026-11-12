package bitbucket

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

const (
	CommitStatusSuccessful = "SUCCESSFUL"
	CommitStatusFailed     = "FAILED"
	CommitStatusInProgress = "INPROGRESS"
	CommitStatusStopped    = "STOPPED"
)

type CommitStatus struct {
	Type        string    `json:"type"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	RefName     string    `json:"refname"`
	CreatedOn   time.Time `json:"created_on"`
	UpdatedOn   time.Time `json:"updated_on"`
}

func (c *Client) ListPullRequestStatuses(ctx context.Context, workspace, repo string, id int) ([]CommitStatus, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/statuses", repoPath(workspace, repo), id)
	return collectPages[CommitStatus](ctx, c, path, 0, "pull request statuses")
}

func (c *Client) ListCommitStatuses(ctx context.Context, workspace, repo, commit string) ([]CommitStatus, error) {
	if commit == "" {
		return nil, fmt.Errorf("commit hash cannot be empty")
	}
	path := fmt.Sprintf("%s/commit/%s/statuses", repoPath(workspace, repo), url.PathEscape(commit))
	return collectPages[CommitStatus](ctx, c, path, 0, "commit statuses")
}
