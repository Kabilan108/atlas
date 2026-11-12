package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type GetIssueOptions struct {
	// Fields limits the returned fields; empty means Jira's default (all
	// navigable fields). Use "*all" for every field.
	Fields []string
	// Expand takes Jira's expand values, e.g. "names", "renderedFields",
	// "changelog", "schema", "transitions".
	Expand []string
}

func (c *Client) GetIssue(ctx context.Context, issueKey string, opts GetIssueOptions) (*Issue, error) {
	q := url.Values{}
	if len(opts.Fields) > 0 {
		q.Set("fields", commaJoin(opts.Fields))
	}
	if len(opts.Expand) > 0 {
		q.Set("expand", commaJoin(opts.Expand))
	}
	var issue Issue
	if err := c.getJSON(ctx, issuePath(issueKey), q, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// ListChangelog returns the issue's complete change history, oldest first.
func (c *Client) ListChangelog(ctx context.Context, issueKey string) ([]ChangelogHistory, error) {
	return collectOffset(0, defaultPageSize, func(startAt, maxResults int) ([]ChangelogHistory, pageInfo, error) {
		var page struct {
			StartAt    int                `json:"startAt"`
			MaxResults int                `json:"maxResults"`
			Total      int                `json:"total"`
			IsLast     *bool              `json:"isLast"`
			Values     []ChangelogHistory `json:"values"`
		}
		err := c.getJSON(ctx, issuePath(issueKey, "changelog"), pageQuery(startAt, maxResults), &page)
		return page.Values, pageInfo{page.StartAt, page.MaxResults, page.Total, page.IsLast}, err
	})
}

type SearchRequest struct {
	JQL           string   `json:"jql"`
	Fields        []string `json:"fields,omitempty"`
	Expand        string   `json:"expand,omitempty"`
	MaxResults    int      `json:"maxResults,omitempty"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
	FieldsByKeys  bool     `json:"fieldsByKeys,omitempty"`
}

// SearchResult is one page of search results. Names and Schema are the
// "names" and "schema" expansions, which Jira returns once per page; they are
// also copied onto each issue that lacks its own, so Issue.FieldByName works.
type SearchResult struct {
	Issues        []Issue                `json:"issues"`
	NextPageToken string                 `json:"nextPageToken,omitempty"`
	IsLast        bool                   `json:"isLast"`
	Names         map[string]string      `json:"names,omitempty"`
	Schema        map[string]FieldSchema `json:"schema,omitempty"`
}

// SearchPage runs one page of an enhanced JQL search. The JQL, including any
// ORDER BY, is sent unchanged.
func (c *Client) SearchPage(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	var res SearchResult
	if err := c.sendJSON(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, req, &res); err != nil {
		return nil, err
	}
	if res.NextPageToken == "" {
		res.IsLast = true
	}
	for i := range res.Issues {
		if res.Issues[i].Names == nil {
			res.Issues[i].Names = res.Names
		}
		if res.Issues[i].Schema == nil {
			res.Issues[i].Schema = res.Schema
		}
	}
	return &res, nil
}

// Search follows nextPageToken until isLast, including across empty pages.
// limit <= 0 returns every match; req.NextPageToken is a starting point and
// req.MaxResults the page size.
func (c *Client) Search(ctx context.Context, req SearchRequest, limit int) ([]Issue, error) {
	if req.MaxResults <= 0 {
		req.MaxResults = defaultPageSize
	}
	seen := map[string]bool{}
	var issues []Issue
	for {
		if limit > 0 && limit-len(issues) < req.MaxResults {
			req.MaxResults = limit - len(issues)
		}
		page, err := c.SearchPage(ctx, req)
		if err != nil {
			return issues, err
		}
		issues = append(issues, page.Issues...)
		if limit > 0 && len(issues) >= limit {
			return issues[:limit], nil
		}
		if page.IsLast {
			return issues, nil
		}
		if seen[page.NextPageToken] {
			return issues, fmt.Errorf("jira: search returned page token %q twice", page.NextPageToken)
		}
		seen[page.NextPageToken] = true
		req.NextPageToken = page.NextPageToken
	}
}

// IssueInput is the body for create and edit. Fields are keyed by field ID;
// Update holds Jira's operation lists (e.g. {"labels": [{"add": "x"}]}).
type IssueInput struct {
	Fields map[string]any              `json:"fields,omitempty"`
	Update map[string][]map[string]any `json:"update,omitempty"`
}

type CreatedIssue struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

func (c *Client) CreateIssue(ctx context.Context, input IssueInput) (*CreatedIssue, error) {
	var created CreatedIssue
	if err := c.sendJSON(ctx, http.MethodPost, "/rest/api/3/issue", nil, input, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

type EditIssueOptions struct {
	// NotifyUsers nil leaves Jira's default (notify).
	NotifyUsers *bool
}

func (c *Client) EditIssue(ctx context.Context, issueKey string, input IssueInput, opts EditIssueOptions) error {
	q := url.Values{}
	if opts.NotifyUsers != nil {
		q.Set("notifyUsers", fmt.Sprint(*opts.NotifyUsers))
	}
	return c.sendJSON(ctx, http.MethodPut, issuePath(issueKey), q, input, nil)
}

type CreateMetaIssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel int    `json:"hierarchyLevel"`
}

// createMetaPage accepts both "values" (what Jira returns) and the
// documented "issueTypes"/"fields" keys.
type createMetaPage[T any] struct {
	StartAt    int   `json:"startAt"`
	MaxResults int   `json:"maxResults"`
	Total      int   `json:"total"`
	IsLast     *bool `json:"isLast"`
	Values     []T   `json:"values"`
	IssueTypes []T   `json:"issueTypes"`
	Fields     []T   `json:"fields"`
}

func (p createMetaPage[T]) items() []T {
	switch {
	case len(p.Values) > 0:
		return p.Values
	case len(p.IssueTypes) > 0:
		return p.IssueTypes
	}
	return p.Fields
}

// getCreateMeta reads up to limit items, one past it to learn whether Jira
// has more, and reports that as truncated.
func getCreateMeta[T any](ctx context.Context, c *Client, path string, limit int) ([]T, bool, error) {
	if limit <= 0 {
		return nil, false, errLimit
	}
	items, err := collectOffset(limit+1, defaultPageSize, func(startAt, maxResults int) ([]T, pageInfo, error) {
		var page createMetaPage[T]
		err := c.getJSON(ctx, path, pageQuery(startAt, maxResults), &page)
		return page.items(), pageInfo{page.StartAt, page.MaxResults, page.Total, page.IsLast}, err
	})
	if err != nil {
		return nil, false, err
	}
	if len(items) > limit {
		return items[:limit], true, nil
	}
	return items, false, nil
}

// CreateMetaIssueTypes lists up to limit issue types a project can create;
// truncated says the project has more.
func (c *Client) CreateMetaIssueTypes(ctx context.Context, projectKey string, limit int) (types []CreateMetaIssueType, truncated bool, err error) {
	return getCreateMeta[CreateMetaIssueType](ctx, c, "/rest/api/3/issue/createmeta/"+escape(projectKey)+"/issuetypes", limit)
}

// CreateMetaFields lists up to limit create-screen fields for a project
// and issue type ID; truncated says the screen has more.
func (c *Client) CreateMetaFields(ctx context.Context, projectKey, issueTypeID string, limit int) (fields []FieldMeta, truncated bool, err error) {
	return getCreateMeta[FieldMeta](ctx, c, "/rest/api/3/issue/createmeta/"+escape(projectKey)+"/issuetypes/"+escape(issueTypeID), limit)
}

// EditMeta lists the fields that can be edited on an issue, with FieldID set
// from the response keys and sorted by name.
func (c *Client) EditMeta(ctx context.Context, issueKey string) ([]FieldMeta, error) {
	var res struct {
		Fields map[string]FieldMeta `json:"fields"`
	}
	if err := c.getJSON(ctx, issuePath(issueKey, "editmeta"), nil, &res); err != nil {
		return nil, err
	}
	return fieldMetaList(res.Fields), nil
}

// ServerInfo returns the site's base URL (for browse links) and version.
func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	var info ServerInfo
	if err := c.getJSON(ctx, "/rest/api/3/serverInfo", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
