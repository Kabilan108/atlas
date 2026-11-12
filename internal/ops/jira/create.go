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
	Parent   string         `json:"parent,omitempty" jsonschema:"parent issue key: the epic for a story, the story for a subtask"`
	Labels   []string       `json:"labels,omitempty"`
	Fields   map[string]any `json:"fields,omitempty" jsonschema:"other fields by display name, e.g. {\"Product\": [\"MCP Data Aggregator\"], \"Software Version\": \"2.4.1\"}; option values by label, users like assignee"`
	Links    []NewLink      `json:"links,omitempty" jsonschema:"issue links to add once created: [{type, target, direction}], as in jira_link"`
}

// NewLink is one link jira_issue_create adds, read as jira_link reads it.
type NewLink struct {
	Type      string        `json:"type"`
	Target    string        `json:"target"`
	Direction LinkDirection `json:"direction,omitempty"`
}

// MaxCreateLinks caps CreateInput.Links.
const MaxCreateLinks = 20

// Created is the receipt for a new issue. Links has one entry per
// requested link.
type Created struct {
	Key   string       `json:"key"`
	ID    string       `json:"id"`
	URL   string       `json:"url"`
	Links []LinkResult `json:"links,omitempty"`
}

// LinkResult is the outcome of one link added after a create.
type LinkResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	Reads  string `json:"reads,omitempty"`
	Error  string `json:"error,omitempty"`
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
	targets, err := linkTargets(in.Links)
	if err != nil {
		return nil, err
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
	linkTypes, err := resolveLinkTypes(ctx, client, in.Links)
	if err != nil {
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
	receipt := &Created{Key: created.Key, ID: created.ID, URL: jiraapi.BrowseURL(site, created.Key)}
	return receipt, receipt.addLinks(ctx, client, in.Links, targets, linkTypes)
}

// linkTargets checks every requested link before the issue is created and
// returns the target keys.
func linkTargets(links []NewLink) ([]string, error) {
	if len(links) > MaxCreateLinks {
		return nil, ops.Errorf(ops.Invalid, "links holds %d links; the limit is %d", len(links), MaxCreateLinks)
	}
	targets := make([]string, len(links))
	for i, l := range links {
		if strings.TrimSpace(l.Type) == "" {
			return nil, ops.Errorf(ops.Invalid, "links[%d]: type is required: give a link type name such as Blocks or Relates", i)
		}
		target, err := issueKey(fmt.Sprintf("links[%d].target", i), l.Target)
		if err != nil {
			return nil, err
		}
		targets[i] = target
	}
	return targets, nil
}

// resolveLinkTypes finds each link's type by name, so a misspelt type
// fails before the issue exists.
func resolveLinkTypes(ctx context.Context, client *jiraapi.Client, links []NewLink) ([]jiraapi.IssueLinkType, error) {
	if len(links) == 0 {
		return nil, nil
	}
	types, err := client.ListIssueLinkTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}
	resolved := make([]jiraapi.IssueLinkType, len(links))
	for i, l := range links {
		if resolved[i], err = jiraapi.ResolveLinkType(types, l.Type); err != nil {
			return nil, invalid(fmt.Errorf("links[%d]: %w", i, err))
		}
	}
	return resolved, nil
}

// addLinks links the new issue to each target in order, recording each
// outcome. The issue exists either way, so a failed link is Partial.
func (c *Created) addLinks(ctx context.Context, client *jiraapi.Client, links []NewLink, targets []string, types []jiraapi.IssueLinkType) error {
	failed := 0
	for i, l := range links {
		result := LinkResult{Index: i, Status: "ok"}
		payload, verb := linkPayload(types[i], c.Key, targets[i], l.Direction)
		var err error
		if targets[i] == c.Key {
			err = fmt.Errorf("cannot link %s to itself", c.Key)
		} else {
			err = client.CreateIssueLink(ctx, payload)
		}
		if err != nil {
			result.Status, result.Error = "failed", err.Error()
			failed++
		} else {
			result.Reads = fmt.Sprintf("%s %s %s", c.Key, verb, targets[i])
		}
		c.Links = append(c.Links, result)
	}
	if err := ops.PartialOf(failed, len(links), "links"); err != nil {
		return fmt.Errorf("created %s, but %w; add the rest with jira_link", c.Key, err)
	}
	return nil
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
