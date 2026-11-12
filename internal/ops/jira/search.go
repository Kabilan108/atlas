package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/jira/adf"
	"github.com/kabilan108/atlas/internal/ops"
)

const (
	defaultSearchLimit = 50
	searchPageSize     = 100
)

var searchColumns = []string{"issuetype", "status", "priority", "assignee", "updated", "summary"}

// SearchInput is the input to jira_search.
type SearchInput struct {
	JQL       string   `json:"jql" jsonschema:"JQL, sent unchanged (ORDER BY included)"`
	Fields    []string `json:"fields,omitempty" jsonschema:"extra columns, by field name or ID"`
	Limit     int      `json:"limit,omitempty" jsonschema:"max issues (default 50, max 1000)"`
	PageToken string   `json:"page_token,omitempty" jsonschema:"next_page_token from an earlier result, to continue"`
}

// SearchResult is the issues a search returned and where it stopped.
type SearchResult struct {
	Issues        []jiraapi.Issue `json:"issues"`
	NextPageToken string          `json:"next_page_token,omitempty"`

	columns []jiraapi.Field
}

var Search = &ops.Op[SearchInput, *SearchResult]{
	Meta: ops.Meta{
		Name:        "jira_search",
		Description: "Search Jira with JQL and list matching issues as a markdown table (key, type, status, priority, assignee, updated, summary, plus any fields you name). Pass next_page_token back as page_token for more.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      search,
	Markdown: (*SearchResult).Markdown,
}

func search(ctx context.Context, env ops.Env, in SearchInput) (*SearchResult, error) {
	if strings.TrimSpace(in.JQL) == "" {
		return nil, ops.Errorf(ops.Invalid, "jql is required")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	result := &SearchResult{}
	fields := append([]string{}, searchColumns...)
	if len(in.Fields) > 0 {
		all, err := client.ListFields(ctx)
		if err != nil {
			return nil, fmt.Errorf("list fields: %w", err)
		}
		ids, err := jiraapi.ResolveFieldIDs(all, in.Fields)
		if err != nil {
			return nil, invalid(err)
		}
		byID := map[string]jiraapi.Field{}
		for _, f := range all {
			byID[f.ID] = f
		}
		for _, id := range ids {
			result.columns = append(result.columns, byID[id])
			fields = append(fields, id)
		}
	}
	limit, err := boundedCount("limit", in.Limit, defaultSearchLimit, MaxSearchResults)
	if err != nil {
		return nil, err
	}
	req := jiraapi.SearchRequest{JQL: in.JQL, Fields: fields, NextPageToken: in.PageToken}
	seen := map[string]bool{}
	for {
		req.MaxResults = min(searchPageSize, limit-len(result.Issues))
		page, err := client.SearchPage(ctx, req)
		if err != nil {
			return nil, err
		}
		result.Issues = append(result.Issues, page.Issues...)
		result.NextPageToken = ""
		if page.IsLast {
			return result, nil
		}
		result.NextPageToken = page.NextPageToken
		if len(result.Issues) >= limit {
			return result, nil
		}
		if seen[page.NextPageToken] {
			return nil, ops.Errorf(ops.Upstream, "jira: search returned page token %q twice", page.NextPageToken)
		}
		seen[page.NextPageToken] = true
		req.NextPageToken = page.NextPageToken
	}
}

// Markdown renders the issues as a table.
func (r *SearchResult) Markdown() (string, error) {
	if len(r.Issues) == 0 {
		return "No issues match.\n", nil
	}
	var b strings.Builder
	header := []string{"Key", "Type", "Status", "Priority", "Assignee", "Updated", "Summary"}
	for _, column := range r.columns {
		header = append(header, cell(column.Name))
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|" + strings.Repeat(" --- |", len(header)) + "\n")
	for _, issue := range r.Issues {
		f := issue.Fields
		row := []string{issue.Key, "", "", "", "", "", f.Summary}
		if f.IssueType != nil {
			row[1] = f.IssueType.Name
		}
		if f.Status != nil {
			row[2] = f.Status.Name
		}
		if f.Priority != nil {
			row[3] = f.Priority.Name
		}
		row[4] = userName(f.Assignee)
		if updated := when(f.Updated); len(updated) >= 10 {
			row[5] = updated[:10]
		}
		for _, column := range r.columns {
			text, err := fieldText(issue.RawFields[column.ID], func(raw json.RawMessage) (string, error) { return adf.ToMarkdown(raw) })
			if err != nil {
				return "", fmt.Errorf("render %s on %s: %w", column.ID, issue.Key, err)
			}
			row = append(row, text)
		}
		for i := range row {
			row[i] = cell(row[i])
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	noun := "issues"
	if len(r.Issues) == 1 {
		noun = "issue"
	}
	fmt.Fprintf(&b, "\n%d %s.", len(r.Issues), noun)
	if r.NextPageToken != "" {
		fmt.Fprintf(&b, " More match: call again with page_token %q.", r.NextPageToken)
	}
	b.WriteString("\n")
	return b.String(), nil
}
