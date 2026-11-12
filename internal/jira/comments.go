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
	return c.CreateComment(ctx, issueKey, CommentInput{Body: body, Visibility: visibility})
}

// CommentInput is the body for CreateComment. Body is an ADF document.
type CommentInput struct {
	Body       json.RawMessage  `json:"body"`
	Visibility *Visibility      `json:"visibility,omitempty"`
	Properties []EntityProperty `json:"properties,omitempty"`
}

// InternalNoteProperty marks a comment on a Service Management issue as an
// internal note. Jira reports the result as Comment.JSDPublic == false.
func InternalNoteProperty() EntityProperty {
	return EntityProperty{Key: "sd.public.comment", Value: json.RawMessage(`{"internal":true}`)}
}

// CreateComment posts a comment through the platform API, which also takes
// entity properties such as InternalNoteProperty. Callers that need a
// comment to be internal must check the returned JSDPublic, which is nil
// when Jira did not report it.
func (c *Client) CreateComment(ctx context.Context, issueKey string, input CommentInput) (*Comment, error) {
	var comment Comment
	if err := c.sendJSON(ctx, http.MethodPost, issuePath(issueKey, "comment"), nil, input, &comment); err != nil {
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
	// AdjustEstimate is "new" (set NewEstimate), "manual" (set ReduceBy),
	// "leave" or "auto"; empty means Jira's default, "auto".
	AdjustEstimate string
	// NewEstimate is the remaining estimate for "new", e.g. "2d".
	NewEstimate string
	// ReduceBy is the amount to cut the remaining estimate by for "manual".
	ReduceBy    string
	NotifyUsers *bool
}

func (o WorklogAddOptions) query() (url.Values, error) {
	q := url.Values{}
	switch o.AdjustEstimate {
	case "", "auto", "leave":
	case "new":
		if o.NewEstimate == "" {
			return nil, fmt.Errorf("jira: adjustEstimate new needs NewEstimate")
		}
	case "manual":
		if o.ReduceBy == "" {
			return nil, fmt.Errorf("jira: adjustEstimate manual needs ReduceBy")
		}
	default:
		return nil, fmt.Errorf("jira: unknown adjustEstimate %q; want new, leave, manual or auto", o.AdjustEstimate)
	}
	if o.NewEstimate != "" && o.AdjustEstimate != "new" {
		return nil, fmt.Errorf("jira: NewEstimate needs adjustEstimate new")
	}
	if o.ReduceBy != "" && o.AdjustEstimate != "manual" {
		return nil, fmt.Errorf("jira: ReduceBy needs adjustEstimate manual")
	}
	if o.AdjustEstimate != "" {
		q.Set("adjustEstimate", o.AdjustEstimate)
	}
	if o.NewEstimate != "" {
		q.Set("newEstimate", o.NewEstimate)
	}
	if o.ReduceBy != "" {
		q.Set("reduceBy", o.ReduceBy)
	}
	return q, nil
}

func (c *Client) AddWorklog(ctx context.Context, issueKey string, input WorklogInput, opts WorklogAddOptions) (*Worklog, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
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
