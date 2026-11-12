package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	MergeStrategyMergeCommit       = "merge_commit"
	MergeStrategySquash            = "squash"
	MergeStrategyFastForward       = "fast_forward"
	MergeStrategySquashFastForward = "squash_fast_forward"
	MergeStrategyRebaseFastForward = "rebase_fast_forward"
	MergeStrategyRebaseMerge       = "rebase_merge"

	MergeTaskPending = "PENDING"
	MergeTaskSuccess = "SUCCESS"
)

var MergeStrategies = []string{
	MergeStrategyMergeCommit,
	MergeStrategySquash,
	MergeStrategyFastForward,
	MergeStrategySquashFastForward,
	MergeStrategyRebaseFastForward,
	MergeStrategyRebaseMerge,
}

var ErrMergePending = errors.New("merge still pending")

const (
	mergePollInitialDelay = 200 * time.Millisecond
	mergePollMaxDelay     = 2 * time.Second
)

// PullRequestMergeOptions configures a merge. An empty Strategy lets Bitbucket
// apply the destination branch's default_merge_strategy; a nil CloseSourceBranch
// keeps the pull request's own close_source_branch setting.
type PullRequestMergeOptions struct {
	Strategy          string
	Message           string
	CloseSourceBranch *bool
	Async             bool
}

type mergeRequest struct {
	Type              string `json:"type"`
	Message           string `json:"message,omitempty"`
	CloseSourceBranch *bool  `json:"close_source_branch,omitempty"`
	MergeStrategy     string `json:"merge_strategy,omitempty"`
}

// MergeResult holds the merged pull request when Bitbucket answered 200, or the
// task-status URL to pass to WaitForMerge when it answered 202.
type MergeResult struct {
	PullRequest  *PullRequest
	TaskLocation string
}

func (r MergeResult) Pending() bool {
	return r.PullRequest == nil && r.TaskLocation != ""
}

type MergeTaskStatus struct {
	TaskStatus  string       `json:"task_status"`
	MergeResult *PullRequest `json:"merge_result,omitempty"`
}

func (c *Client) MergePullRequest(ctx context.Context, workspace, repo string, id int, opts PullRequestMergeOptions) (*MergeResult, error) {
	if opts.Strategy != "" && !slices.Contains(MergeStrategies, opts.Strategy) {
		return nil, fmt.Errorf("unknown merge strategy %q (valid: %s)", opts.Strategy, strings.Join(MergeStrategies, ", "))
	}

	path := fmt.Sprintf("%s/pullrequests/%d/merge", repoPath(workspace, repo), id)
	if opts.Async {
		path += "?async=true"
	}

	resp, data, err := c.send(ctx, http.MethodPost, path, mergeRequest{
		Type:              "pullrequest_merge_parameters",
		Message:           opts.Message,
		CloseSourceBranch: opts.CloseSourceBranch,
		MergeStrategy:     opts.Strategy,
	})
	if err != nil {
		return nil, err
	}
	c.invalidatePRCache()

	if resp.StatusCode == http.StatusAccepted {
		location := resp.Header.Get("Location")
		if location == "" {
			return nil, fmt.Errorf("merge accepted but Bitbucket returned no task-status Location")
		}
		return &MergeResult{TaskLocation: location}, nil
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse merge response: %w", err)
	}
	return &MergeResult{PullRequest: &pr}, nil
}

func (c *Client) GetMergeTaskStatus(ctx context.Context, location string) (*MergeTaskStatus, error) {
	path, err := c.apiPathFromLocation(location)
	if err != nil {
		return nil, err
	}

	var status MergeTaskStatus
	if err := c.getJSON(ctx, path, &status, "merge task status"); err != nil {
		return nil, err
	}
	return &status, nil
}

// WaitForMerge polls a merge task until it succeeds, fails, or the deadline
// passes. Cancelling ctx or reaching the deadline also aborts an in-flight
// poll. On timeout the error wraps ErrMergePending and names the task URL.
// The returned pull request is nil when Bitbucket omits merge_result.
func (c *Client) WaitForMerge(ctx context.Context, location string, deadline time.Time) (*PullRequest, error) {
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	delay := mergePollInitialDelay
	for {
		if pollCtx.Err() != nil {
			return nil, mergeWaitError(ctx, location)
		}
		status, err := c.GetMergeTaskStatus(pollCtx, location)
		if err != nil {
			if pollCtx.Err() != nil {
				return nil, mergeWaitError(ctx, location)
			}
			return nil, err
		}

		switch status.TaskStatus {
		case MergeTaskSuccess:
			c.invalidatePRCache()
			return status.MergeResult, nil
		case MergeTaskPending, "":
		default:
			return nil, fmt.Errorf("merge task finished with status %s", status.TaskStatus)
		}

		if err := sleepContext(pollCtx, delay); err != nil {
			return nil, mergeWaitError(ctx, location)
		}
		delay = min(delay*2, mergePollMaxDelay)
	}
}

// mergeWaitError reports the caller's cancellation as is and anything else,
// which can only be the polling deadline, as a still-pending merge.
func mergeWaitError(ctx context.Context, location string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w at deadline; task status: %s", ErrMergePending, location)
}

// apiPathFromLocation turns a Location header into a path under the client's
// base URL and refuses anything else, so credentials never leave the API.
func (c *Client) apiPathFromLocation(location string) (string, error) {
	path, err := c.apiPath(location)
	if err != nil {
		return "", fmt.Errorf("task-status location: %w", err)
	}
	return path, nil
}
