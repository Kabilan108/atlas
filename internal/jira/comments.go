package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

type CommentListOptions struct {
	// OrderBy is "created" or "-created".
	OrderBy string
	// Expand takes "renderedBody" and/or "properties".
	Expand []string
	// Limit caps the number of comments; <= 0 returns all.
	Limit int
}

// ListCommentsPage returns one page of comments starting at startAt.
func (c *Client) ListCommentsPage(ctx context.Context, issueKey string, startAt, maxResults int, opts CommentListOptions) (*CommentPage, error) {
	q := pageQuery(startAt, maxResults)
	if opts.OrderBy != "" {
		q.Set("orderBy", opts.OrderBy)
	}
	if len(opts.Expand) > 0 {
		q.Set("expand", commaJoin(opts.Expand))
	}
	var page CommentPage
	if err := c.getJSON(ctx, issuePath(issueKey, "comment"), q, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) ListComments(ctx context.Context, issueKey string, opts CommentListOptions) ([]Comment, error) {
	return collectOffset(opts.Limit, defaultPageSize, func(startAt, maxResults int) ([]Comment, pageInfo, error) {
		page, err := c.ListCommentsPage(ctx, issueKey, startAt, maxResults, opts)
		if err != nil {
			return nil, pageInfo{}, err
		}
		return page.Comments, pageInfo{page.StartAt, page.MaxResults, page.Total, nil}, nil
	})
}

// AddComment posts an ADF body (e.g. from adf.FromMarkdown). Visibility may
// be nil.
func (c *Client) AddComment(ctx context.Context, issueKey string, body json.RawMessage, visibility *Visibility) (*Comment, error) {
	payload := struct {
		Body       json.RawMessage `json:"body"`
		Visibility *Visibility     `json:"visibility,omitempty"`
	}{body, visibility}
	var comment Comment
	if err := c.sendJSON(ctx, http.MethodPost, issuePath(issueKey, "comment"), nil, payload, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (c *Client) DeleteComment(ctx context.Context, issueKey, commentID string) error {
	return c.sendJSON(ctx, http.MethodDelete, issuePath(issueKey, "comment", escape(commentID)), nil, nil, nil)
}

type WorklogListOptions struct {
	// StartedAfter and StartedBefore are Unix milliseconds; 0 means unset.
	StartedAfter  int64
	StartedBefore int64
}

// ListWorklogs returns every worklog on the issue.
func (c *Client) ListWorklogs(ctx context.Context, issueKey string, opts WorklogListOptions) ([]Worklog, error) {
	return collectOffset(0, 1000, func(startAt, maxResults int) ([]Worklog, pageInfo, error) {
		q := pageQuery(startAt, maxResults)
		if opts.StartedAfter > 0 {
			q.Set("startedAfter", fmt.Sprint(opts.StartedAfter))
		}
		if opts.StartedBefore > 0 {
			q.Set("startedBefore", fmt.Sprint(opts.StartedBefore))
		}
		var page struct {
			StartAt    int       `json:"startAt"`
			MaxResults int       `json:"maxResults"`
			Total      int       `json:"total"`
			Worklogs   []Worklog `json:"worklogs"`
		}
		err := c.getJSON(ctx, issuePath(issueKey, "worklog"), q, &page)
		return page.Worklogs, pageInfo{page.StartAt, page.MaxResults, page.Total, nil}, err
	})
}

// WorklogInput adds a worklog. Started must be in Jira's format (see
// FormatTime); set TimeSpent ("1h 30m") or TimeSpentSeconds. Comment is an
// optional ADF document.
type WorklogInput struct {
	Started          string          `json:"started"`
	TimeSpent        string          `json:"timeSpent,omitempty"`
	TimeSpentSeconds int             `json:"timeSpentSeconds,omitempty"`
	Comment          json.RawMessage `json:"comment,omitempty"`
	Visibility       *Visibility     `json:"visibility,omitempty"`
}

type WorklogAddOptions struct {
	// AdjustEstimate is "new", "leave", "manual" or "auto" (Jira's default).
	AdjustEstimate string
	NotifyUsers    *bool
}

func (c *Client) AddWorklog(ctx context.Context, issueKey string, input WorklogInput, opts WorklogAddOptions) (*Worklog, error) {
	q := url.Values{}
	if opts.AdjustEstimate != "" {
		q.Set("adjustEstimate", opts.AdjustEstimate)
	}
	if opts.NotifyUsers != nil {
		q.Set("notifyUsers", fmt.Sprint(*opts.NotifyUsers))
	}
	var w Worklog
	if err := c.sendJSON(ctx, http.MethodPost, issuePath(issueKey, "worklog"), q, input, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

type AttachmentFile struct {
	Name    string
	Content io.Reader
}

// UploadAttachments sends files as one multipart request and returns the
// attachments Jira created.
func (c *Client) UploadAttachments(ctx context.Context, issueKey string, files []AttachmentFile) ([]Attachment, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("jira: no files to attach")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range files {
		part, err := w.CreateFormFile("file", f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, f.Content); err != nil {
			return nil, fmt.Errorf("jira: read %s: %w", f.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	path := issuePath(issueKey, "attachments")
	req, err := c.newRequest(ctx, http.MethodPost, path, nil, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check")
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var attachments []Attachment
	if err := decode(data, &attachments, path); err != nil {
		return nil, err
	}
	return attachments, nil
}
