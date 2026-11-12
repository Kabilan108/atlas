package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

// ListTransitions returns the transitions available on an issue, with each
// transition's screen fields (expand=transitions.fields).
func (c *Client) ListTransitions(ctx context.Context, issueKey string) ([]Transition, error) {
	q := url.Values{}
	q.Set("expand", "transitions.fields")
	var res struct {
		Transitions []Transition `json:"transitions"`
	}
	if err := c.getJSON(ctx, issuePath(issueKey, "transitions"), q, &res); err != nil {
		return nil, err
	}
	return res.Transitions, nil
}

type TransitionInput struct {
	Fields map[string]any              `json:"fields,omitempty"`
	Update map[string][]map[string]any `json:"update,omitempty"`
}

func (c *Client) DoTransition(ctx context.Context, issueKey, transitionID string, input TransitionInput) error {
	body := struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
		TransitionInput
	}{TransitionInput: input}
	body.Transition.ID = transitionID
	return c.sendJSON(ctx, http.MethodPost, issuePath(issueKey, "transitions"), nil, body, nil)
}

func (c *Client) ListIssueLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	var res struct {
		IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
	}
	if err := c.getJSON(ctx, "/rest/api/3/issueLinkType", nil, &res); err != nil {
		return nil, err
	}
	return res.IssueLinkTypes, nil
}

// LinkInput links two issues. For a type whose outward text is "blocks",
// OutwardIssue blocks InwardIssue. Comment is an optional ADF document.
type LinkInput struct {
	TypeName     string
	InwardIssue  string
	OutwardIssue string
	Comment      any
}

func (c *Client) CreateIssueLink(ctx context.Context, input LinkInput) error {
	type keyRef struct {
		Key string `json:"key"`
	}
	body := map[string]any{
		"type":         map[string]string{"name": input.TypeName},
		"inwardIssue":  keyRef{input.InwardIssue},
		"outwardIssue": keyRef{input.OutwardIssue},
	}
	if input.Comment != nil {
		body["comment"] = map[string]any{"body": input.Comment}
	}
	return c.sendJSON(ctx, http.MethodPost, "/rest/api/3/issueLink", nil, body, nil)
}

// AssignIssue sets the assignee by account ID; an empty accountID unassigns.
func (c *Client) AssignIssue(ctx context.Context, issueKey, accountID string) error {
	body := map[string]any{"accountId": nil}
	if accountID != "" {
		body["accountId"] = accountID
	}
	return c.sendJSON(ctx, http.MethodPut, issuePath(issueKey, "assignee"), nil, body, nil)
}

type AssignableSearch struct {
	IssueKey   string
	ProjectKey string
	// Query matches display name and email address.
	Query      string
	MaxResults int
}

func (c *Client) SearchAssignableUsers(ctx context.Context, s AssignableSearch) ([]User, error) {
	q := url.Values{}
	if s.IssueKey != "" {
		q.Set("issueKey", s.IssueKey)
	}
	if s.ProjectKey != "" {
		q.Set("project", s.ProjectKey)
	}
	if s.Query != "" {
		q.Set("query", s.Query)
	}
	if s.MaxResults > 0 {
		q.Set("maxResults", fmt.Sprint(s.MaxResults))
	}
	var users []User
	if err := c.getJSON(ctx, "/rest/api/3/user/assignable/search", q, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) SearchUsers(ctx context.Context, query string, maxResults int) ([]User, error) {
	q := url.Values{}
	q.Set("query", query)
	if maxResults > 0 {
		q.Set("maxResults", fmt.Sprint(maxResults))
	}
	var users []User
	if err := c.getJSON(ctx, "/rest/api/3/user/search", q, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) Myself(ctx context.Context) (*User, error) {
	var u User
	if err := c.getJSON(ctx, "/rest/api/3/myself", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

type ProjectListOptions struct {
	Query   string
	OrderBy string
	Limit   int
}

func (c *Client) ListProjects(ctx context.Context, opts ProjectListOptions) ([]Project, error) {
	return collectOffset(opts.Limit, 50, func(startAt, maxResults int) ([]Project, pageInfo, error) {
		q := pageQuery(startAt, maxResults)
		if opts.Query != "" {
			q.Set("query", opts.Query)
		}
		if opts.OrderBy != "" {
			q.Set("orderBy", opts.OrderBy)
		}
		var page struct {
			StartAt    int       `json:"startAt"`
			MaxResults int       `json:"maxResults"`
			Total      int       `json:"total"`
			IsLast     *bool     `json:"isLast"`
			Values     []Project `json:"values"`
		}
		err := c.getJSON(ctx, "/rest/api/3/project/search", q, &page)
		return page.Values, pageInfo{page.StartAt, page.MaxResults, page.Total, page.IsLast}, err
	})
}

func (c *Client) GetProject(ctx context.Context, projectKey string) (*Project, error) {
	var p Project
	if err := c.getJSON(ctx, "/rest/api/3/project/"+escape(projectKey), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func fieldMetaList(fields map[string]FieldMeta) []FieldMeta {
	list := make([]FieldMeta, 0, len(fields))
	for id, f := range fields {
		if f.FieldID == "" {
			f.FieldID = id
		}
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID() < list[j].ID()
	})
	return list
}
