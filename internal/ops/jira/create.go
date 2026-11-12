package jira

import (
	"context"
	"fmt"
	"slices"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// CreateInput is the input to jira_issue_create.
type CreateInput struct {
	Project  string         `json:"project" jsonschema:"project key, e.g. CSM"`
	Type     string         `json:"type" jsonschema:"issue type name, e.g. Bug"`
	Summary  string         `json:"summary"`
	Body     string         `json:"body,omitempty" jsonschema:"description, markdown"`
	Priority string         `json:"priority,omitempty" jsonschema:"priority name"`
	Assignee string         `json:"assignee,omitempty" jsonschema:"me, email, name or account ID"`
	Parent   string         `json:"parent,omitempty" jsonschema:"parent issue key"`
	Labels   []string       `json:"labels,omitempty"`
	Fields   map[string]any `json:"fields,omitempty" jsonschema:"other fields by display name, e.g. {\"Product\": [\"MCP Data Aggregator\"], \"Software Version\": \"2.4.1\"}; option values by label, users like assignee"`
}

// Created is the receipt for a new issue.
type Created struct {
	Key string `json:"key"`
	ID  string `json:"id"`
	URL string `json:"url"`
}

var Create = &ops.Op[CreateInput, *Created]{
	Meta: ops.Meta{
		Name: "jira_issue_create",
		Description: "Create a Jira issue. Name fields by their display name (call jira_fields with the project and type to see " +
			"them, which are required, and allowed values). Returns the key and URL.",
		Surfaces: ops.Both,
	},
	Run: create,
}

func create(ctx context.Context, env ops.Env, in CreateInput) (*Created, error) {
	project, err := projectKey(in.Project)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Type) == "" {
		return nil, ops.Errorf(ops.Invalid, "type is required: give an issue type name such as Bug")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return nil, ops.Errorf(ops.Invalid, "summary is required")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	issueType, err := createIssueType(ctx, client, project, in.Type)
	if err != nil {
		return nil, err
	}
	metas, truncated, err := client.CreateMetaFields(ctx, project, issueType.ID, MaxCreateMetaFields)
	if err != nil {
		return nil, fmt.Errorf("fetch create fields for %s %s: %w", project, issueType.Name, err)
	}
	createScreen := screen{metas: metas, truncated: truncated}
	fields := map[string]any{
		"project":   map[string]any{"key": project},
		"issuetype": map[string]any{"id": issueType.ID},
		"summary":   strings.TrimSpace(in.Summary),
	}
	if in.Body != "" {
		if fields["description"], err = markdownBody("body", in.Body); err != nil {
			return nil, err
		}
	}
	if in.Priority != "" {
		meta, err := createScreen.resolve("priority")
		switch {
		case err != nil && truncated:
			return nil, err
		case err != nil:
			return nil, ops.Errorf(ops.Invalid, "priority is not on the %s %s create screen", project, issueType.Name)
		}
		if fields["priority"], err = jiraapi.ShapeFieldValue(meta, in.Priority); err != nil {
			return nil, invalid(err)
		}
	}
	scope := jiraapi.AssignableSearch{ProjectKey: project}
	if in.Assignee != "" {
		user, err := resolveUser(ctx, client, in.Assignee, scope)
		if err != nil {
			return nil, err
		}
		fields["assignee"] = map[string]any{"accountId": user.AccountID}
	}
	if in.Parent != "" {
		parent, err := issueKey("parent", in.Parent)
		if err != nil {
			return nil, err
		}
		fields["parent"] = map[string]any{"key": parent}
	}
	if len(in.Labels) > 0 {
		fields["labels"] = in.Labels
	}
	if err := shapeFields(ctx, client, createScreen, in.Fields, fields, scope); err != nil {
		return nil, err
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	created, err := client.CreateIssue(ctx, jiraapi.IssueInput{Fields: fields})
	if err != nil {
		return nil, withFieldNames(err, metas)
	}
	return &Created{Key: created.Key, ID: created.ID, URL: jiraapi.BrowseURL(site, created.Key)}, nil
}

func createIssueType(ctx context.Context, client *jiraapi.Client, project, name string) (jiraapi.CreateMetaIssueType, error) {
	types, truncated, err := client.CreateMetaIssueTypes(ctx, project, MaxIssueTypes)
	if err != nil {
		return jiraapi.CreateMetaIssueType{}, fmt.Errorf("fetch issue types for %s: %w", project, err)
	}
	return resolveIssueType(types, truncated, name)
}

// resolveIssueType finds name among types; truncated says the project has
// more types than were listed.
func resolveIssueType(types []jiraapi.CreateMetaIssueType, truncated bool, name string) (jiraapi.CreateMetaIssueType, error) {
	name = strings.TrimSpace(name)
	valid := make([]string, len(types))
	for i, t := range types {
		valid[i] = t.Name
	}
	for _, match := range []func(jiraapi.CreateMetaIssueType) bool{
		func(t jiraapi.CreateMetaIssueType) bool { return t.ID == name },
		func(t jiraapi.CreateMetaIssueType) bool { return t.Name == name },
		func(t jiraapi.CreateMetaIssueType) bool { return strings.EqualFold(t.Name, name) },
	} {
		for _, t := range types {
			if match(t) {
				return t, nil
			}
		}
	}
	if truncated {
		return jiraapi.CreateMetaIssueType{}, ops.Errorf(ops.Invalid,
			"issue type %q is not among the first %d issue types Jira listed, and Jira has more; atlas reads no further, so it can't use this type",
			name, len(types))
	}
	slices.Sort(valid)
	return jiraapi.CreateMetaIssueType{}, ops.Errorf(ops.Invalid, "%w", &jiraapi.ResolveError{Kind: "issue type", Name: name, Candidates: valid})
}
