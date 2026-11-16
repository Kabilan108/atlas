package jira

import (
	"context"
	"errors"
	"fmt"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// worklogDeleteEstimate is the adjustEstimate every worklog delete sends.
// jira_worklog_add logs with Jira's default, auto, which takes the time off
// the remaining estimate; auto on a delete puts it back, so deleting a wrong
// entry undoes logging it.
const worklogDeleteEstimate = "auto"

// WorklogDeleteInput is the input to jira_worklog_delete.
type WorklogDeleteInput struct {
	IssueRef
	IDs []string `json:"ids" jsonschema:"worklog IDs on that issue, up to 100"`
}

// WorklogDeleted says what happened to one worklog. Author, Started and
// TimeSpent are filled when atlas could read the worklog first.
type WorklogDeleted struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Author    string `json:"author,omitempty"`
	Started   string `json:"started,omitempty"`
	TimeSpent string `json:"time_spent,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WorklogDeleteResult has one result per ID, in input order.
type WorklogDeleteResult struct {
	Key     string           `json:"key"`
	Results []WorklogDeleted `json:"results"`
}

var WorklogDelete = &ops.Op[WorklogDeleteInput, *WorklogDeleteResult]{
	Meta: ops.Meta{
		Name: "jira_worklog_delete",
		Description: "Delete worklogs from one Jira issue; Jira adds their time back to the remaining estimate. " +
			"Returns a status per ID with the worklog's author, start and time spent.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run: worklogDelete,
}

func worklogDelete(ctx context.Context, env ops.Env, in WorklogDeleteInput) (*WorklogDeleteResult, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	ids, err := worklogIDs(in.IDs)
	if err != nil {
		return nil, err
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	result := &WorklogDeleteResult{Key: key, Results: make([]WorklogDeleted, len(ids))}
	failed := 0
	var lastErr error
	for i, id := range ids {
		r := &result.Results[i]
		r.ID = id
		if err := deleteWorklog(ctx, client, key, r); err != nil {
			r.Status, r.Error = StatusFailed, err.Error()
			failed++
			lastErr = err
			continue
		}
		r.Status = StatusOK
	}
	if len(ids) == 1 && failed == 1 {
		return nil, lastErr
	}
	return result, ops.PartialOf(failed, len(ids), "worklog deletes")
}

// deleteWorklog reads r's worklog for the receipt, then deletes it.
func deleteWorklog(ctx context.Context, client *jiraapi.Client, key string, r *WorklogDeleted) error {
	const permission = "Delete Own Worklogs (Delete All Worklogs for someone else's)"
	w, err := client.GetWorklog(ctx, key, r.ID)
	if err != nil {
		return deleteError(err, "worklog "+r.ID, key, permission)
	}
	r.Author, r.Started, r.TimeSpent = userName(w.Author), w.Started, w.TimeSpent
	if r.TimeSpent == "" {
		r.TimeSpent = formatSeconds(w.TimeSpentSeconds)
	}
	if err := client.DeleteWorklog(ctx, key, r.ID, worklogDeleteEstimate); err != nil {
		return deleteError(err, "worklog "+r.ID, key, permission)
	}
	return nil
}

func worklogIDs(raw []string) ([]string, error) {
	if len(raw) == 0 || len(raw) > MaxWorklogDeletes {
		return nil, ops.Errorf(ops.Invalid, "ids must hold 1 to %d worklog IDs", MaxWorklogDeletes)
	}
	seen := make(map[string]bool, len(raw))
	ids := make([]string, len(raw))
	for i, s := range raw {
		id, err := numericID(fmt.Sprintf("ids[%d]", i), "worklog", s)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, ops.Errorf(ops.Invalid, "ids[%d]: worklog %s is listed twice", i, id)
		}
		seen[id] = true
		ids[i] = id
	}
	return ids, nil
}

// CommentDeleteInput is the input to jira_comment_delete.
type CommentDeleteInput struct {
	IssueRef
	ID string `json:"id" jsonschema:"comment ID"`
}

// CommentDeleted is the receipt for a deleted comment.
type CommentDeleted struct {
	Key      string `json:"key"`
	ID       string `json:"id"`
	Author   string `json:"author,omitempty"`
	Created  string `json:"created,omitempty"`
	Internal bool   `json:"internal"`
}

// CommentDelete is MCP-only as a tool; the CLI reaches it through
// `atlas jira comment --delete`, which is bound to jira_comment.
var CommentDelete = &ops.Op[CommentDeleteInput, *CommentDeleted]{
	Meta: ops.Meta{
		Name:        "jira_comment_delete",
		Description: "Delete a comment or internal note from a Jira issue. Returns its author, created time and whether it was internal.",
		Surfaces:    ops.MCP,
		Destructive: true,
	},
	Run: commentDelete,
}

func commentDelete(ctx context.Context, env ops.Env, in CommentDeleteInput) (*CommentDeleted, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	id, err := numericID("id", "comment", in.ID)
	if err != nil {
		return nil, err
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	const permission = "Delete Own Comments (Delete All Comments for someone else's)"
	comment, err := client.GetComment(ctx, key, id)
	if err != nil {
		return nil, deleteError(err, "comment "+id, key, permission)
	}
	if err := client.DeleteComment(ctx, key, id); err != nil {
		return nil, deleteError(err, "comment "+id, key, permission)
	}
	return &CommentDeleted{
		Key:      key,
		ID:       id,
		Author:   userName(comment.Author),
		Created:  comment.Created,
		Internal: comment.JSDPublic != nil && !*comment.JSDPublic,
	}, nil
}

func numericID(field, noun, raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if !issueIDPattern.MatchString(id) {
		return "", ops.Errorf(ops.Invalid, "invalid %s %q: expected a numeric %s ID", field, raw, noun)
	}
	return id, nil
}

// deleteError explains a 404 or 403 from reading or deleting thing on key;
// permission names the Jira permission a delete needs.
func deleteError(err error, thing, key, permission string) error {
	switch {
	case errors.Is(err, jiraapi.ErrNotFound):
		return ops.Errorf(ops.NotFound, "%s not found on %s, or you cannot see that issue: %w", thing, key, err)
	case errors.Is(err, jiraapi.ErrForbidden):
		return ops.Errorf(ops.Auth, "jira refused access to %s on %s; deleting it needs the %s permission: %w", thing, key, permission, err)
	}
	return err
}
