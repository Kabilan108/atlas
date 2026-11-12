package jira

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
)

// servicePage is Service Management's pagination envelope, which differs from
// the platform API's.
type servicePage[T any] struct {
	Size       int  `json:"size"`
	Start      int  `json:"start"`
	Limit      int  `json:"limit"`
	IsLastPage bool `json:"isLastPage"`
	Values     []T  `json:"values"`
}

func getServicePages[T any](ctx context.Context, c *Client, path string, base url.Values, limit int) ([]T, error) {
	return collectOffset(limit, 50, func(start, size int) ([]T, pageInfo, error) {
		q := url.Values{}
		maps.Copy(q, base)
		q.Set("start", fmt.Sprint(start))
		q.Set("limit", fmt.Sprint(size))
		var page servicePage[T]
		err := c.getJSON(ctx, path, q, &page)
		isLast := page.IsLastPage
		return page.Values, pageInfo{StartAt: page.Start, MaxResults: page.Limit, IsLast: &isLast}, err
	})
}

func (c *Client) ListServiceDesks(ctx context.Context) ([]ServiceDesk, error) {
	return getServicePages[ServiceDesk](ctx, c, "/rest/servicedeskapi/servicedesk", nil, 0)
}

// CreateRequestComment adds a comment to a Service Management request.
// public=false makes it an internal note visible to agents only. body is
// plain text.
func (c *Client) CreateRequestComment(ctx context.Context, issueKey, body string, public bool) (*RequestComment, error) {
	payload := struct {
		Body   string `json:"body"`
		Public bool   `json:"public"`
	}{body, public}
	var comment RequestComment
	path := "/rest/servicedeskapi/request/" + escape(issueKey) + "/comment"
	if err := c.sendJSON(ctx, http.MethodPost, path, nil, payload, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

type RequestCommentListOptions struct {
	// Public and Internal filter by visibility; nil leaves Jira's default
	// (both included).
	Public   *bool
	Internal *bool
	Limit    int
}

func (c *Client) ListRequestComments(ctx context.Context, issueKey string, opts RequestCommentListOptions) ([]RequestComment, error) {
	q := url.Values{}
	if opts.Public != nil {
		q.Set("public", fmt.Sprint(*opts.Public))
	}
	if opts.Internal != nil {
		q.Set("internal", fmt.Sprint(*opts.Internal))
	}
	path := "/rest/servicedeskapi/request/" + escape(issueKey) + "/comment"
	return getServicePages[RequestComment](ctx, c, path, q, opts.Limit)
}
