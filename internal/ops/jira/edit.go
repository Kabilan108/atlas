package jira

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/jira/adf"
	"github.com/kabilan108/atlas/internal/ops"
)

// BodyMode says how jira_issue_edit applies a new description.
type BodyMode string

const (
	BodyReplace BodyMode = "replace"
	BodyAppend  BodyMode = "append"
)

func (BodyMode) Enum() []string {
	return []string{string(BodyReplace), string(BodyAppend)}
}

// EditInput is the input to jira_issue_edit.
type EditInput struct {
	IssueRef
	Summary  string         `json:"summary,omitempty"`
	Body     string         `json:"body,omitempty" jsonschema:"markdown"`
	BodyMode BodyMode       `json:"body_mode,omitempty" jsonschema:"replace (default) or append"`
	Force    bool           `json:"force,omitempty" jsonschema:"replace even if content is lost"`
	Labels   []string       `json:"labels,omitempty" jsonschema:"to add; -name removes"`
	Assignee string         `json:"assignee,omitempty" jsonschema:"me, email, name, account ID, or none"`
	Fields   map[string]any `json:"fields,omitempty" jsonschema:"by display name; null clears"`
}

// Edited is the receipt for an edit.
type Edited struct {
	Key     string   `json:"key"`
	URL     string   `json:"url"`
	Changed []string `json:"changed"`
}

var Edit = &ops.Op[EditInput, *Edited]{
	Meta: ops.Meta{
		Name: "jira_issue_edit",
		Description: "Edit a Jira issue: summary, description, labels, assignee, fields by display name (see jira_fields). " +
			"Replacing one with media, complex tables etc. needs force; body_mode append keeps it.",
		Surfaces: ops.Both,
	},
	Run: edit,
}

func edit(ctx context.Context, env ops.Env, in EditInput) (*Edited, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	if in.Summary == "" && in.Body == "" && len(in.Labels) == 0 && in.Assignee == "" && len(in.Fields) == 0 {
		return nil, ops.Errorf(ops.Invalid, "nothing to change: set summary, body, labels, assignee or fields")
	}
	if in.Body == "" && (in.BodyMode != "" || in.Force) {
		return nil, ops.Errorf(ops.Invalid, "body_mode and force apply only with body")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	input := jiraapi.IssueInput{Fields: map[string]any{}, Update: map[string][]map[string]any{}}
	var changed []string
	if in.Summary != "" {
		input.Fields["summary"] = strings.TrimSpace(in.Summary)
		changed = append(changed, "summary")
	}
	if in.Body != "" {
		description, note, err := editedDescription(ctx, client, key, in)
		if err != nil {
			return nil, err
		}
		input.Fields["description"] = description
		changed = append(changed, note)
	}
	for _, label := range in.Labels {
		label = strings.TrimSpace(label)
		op := "add"
		if name, ok := strings.CutPrefix(label, "-"); ok {
			op, label = "remove", name
		}
		if label == "" || strings.ContainsFunc(label, func(r rune) bool { return r == ' ' }) {
			return nil, ops.Errorf(ops.Invalid, "invalid label %q: labels cannot be empty or contain spaces", label)
		}
		input.Update["labels"] = append(input.Update["labels"], map[string]any{op: label})
	}
	if len(in.Labels) > 0 {
		changed = append(changed, "labels")
	}
	scope := jiraapi.AssignableSearch{IssueKey: key}
	if in.Assignee != "" {
		if strings.EqualFold(strings.TrimSpace(in.Assignee), unassigned) {
			input.Fields["assignee"] = nil
		} else {
			user, err := resolveUser(ctx, client, in.Assignee, scope)
			if err != nil {
				return nil, err
			}
			input.Fields["assignee"] = map[string]any{"accountId": user.AccountID}
		}
		changed = append(changed, "assignee")
	}
	var metas []jiraapi.FieldMeta
	if len(in.Fields) > 0 {
		if metas, err = client.EditMeta(ctx, key); err != nil {
			return nil, fmt.Errorf("fetch edit fields for %s: %w", key, err)
		}
		before := maps.Clone(input.Fields)
		if err := shapeFields(ctx, client, screen{metas: metas}, in.Fields, input.Fields, scope); err != nil {
			return nil, err
		}
		for _, id := range slices.Sorted(maps.Keys(input.Fields)) {
			if _, ok := before[id]; !ok {
				changed = append(changed, fieldName(metas, id))
			}
		}
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := client.EditIssue(ctx, key, input, jiraapi.EditIssueOptions{}); err != nil {
		return nil, withFieldNames(err, metas)
	}
	return &Edited{Key: key, URL: jiraapi.BrowseURL(site, key), Changed: changed}, nil
}

// editedDescription builds the new description: the current one with the
// body's blocks after it, or the body alone once replacing it is safe.
func editedDescription(ctx context.Context, client *jiraapi.Client, key string, in EditInput) (any, string, error) {
	body, err := markdownBody("body", in.Body)
	if err != nil {
		return nil, "", err
	}
	if in.BodyMode != BodyAppend && in.Force {
		return body, "description", nil
	}
	issue, err := client.GetIssue(ctx, key, jiraapi.GetIssueOptions{Fields: []string{"description"}})
	if err != nil {
		return nil, "", err
	}
	current := issue.Fields.Description
	if in.BodyMode == BodyAppend {
		merged, err := adf.Append(current, body)
		if err != nil {
			return nil, "", ops.Errorf(ops.Invalid, "append to %s's description: %w", key, err)
		}
		return merged, "description (appended)", nil
	}
	lossy, err := adf.Approximate(current)
	if err != nil {
		return nil, "", fmt.Errorf("read %s's description: %w", key, err)
	}
	if lossy.Total() > 0 {
		return nil, "", ops.Errorf(ops.Invalid,
			"%s's description has content markdown can't hold (%s); replacing it would lose that. Use body_mode append, or force to replace anyway", key, lossy)
	}
	return body, "description", nil
}

func fieldName(metas []jiraapi.FieldMeta, id string) string {
	for _, meta := range metas {
		if meta.ID() == id {
			return meta.Name
		}
	}
	return id
}
