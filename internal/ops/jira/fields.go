package jira

import (
	"context"
	"fmt"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

const fieldsAllowedValues = 50

// FieldsInput is the input to jira_fields.
type FieldsInput struct {
	Project string `json:"project,omitempty" jsonschema:"project key, for create fields (omit type to list issue types)"`
	Type    string `json:"type,omitempty" jsonschema:"issue type name, with project"`
	Issue   string `json:"issue,omitempty" jsonschema:"issue key, for the fields that issue can edit (instead of project)"`
}

// FieldsResult is a screen's fields, or a project's issue types when no
// type was given.
type FieldsResult struct {
	Project    string                        `json:"project,omitempty"`
	Type       string                        `json:"type,omitempty"`
	Issue      string                        `json:"issue,omitempty"`
	IssueTypes []jiraapi.CreateMetaIssueType `json:"issue_types,omitempty"`
	Fields     []jiraapi.FieldMeta           `json:"fields,omitempty"`
}

var Fields = &ops.Op[FieldsInput, *FieldsResult]{
	Meta: ops.Meta{
		Name: "jira_fields",
		Description: "List the fields for creating a project's issue type (project + type) or editing an issue (issue): " +
			"display name, ID, type, required, and allowed values. Other jira tools take fields by these names.",
		Surfaces: ops.Both,
		ReadOnly: true,
	},
	Run:      fields,
	Markdown: (*FieldsResult).Markdown,
}

func fields(ctx context.Context, env ops.Env, in FieldsInput) (*FieldsResult, error) {
	switch {
	case in.Issue != "" && (in.Project != "" || in.Type != ""):
		return nil, ops.Errorf(ops.Invalid, "give issue, or project and type, not both")
	case in.Issue == "" && in.Project == "":
		return nil, ops.Errorf(ops.Invalid, "give project (and type) for create fields, or issue for edit fields")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	if in.Issue != "" {
		key, err := issueKey("issue", in.Issue)
		if err != nil {
			return nil, err
		}
		metas, err := client.EditMeta(ctx, key)
		if err != nil {
			return nil, err
		}
		return &FieldsResult{Issue: key, Fields: metas}, nil
	}
	project, err := projectKey(in.Project)
	if err != nil {
		return nil, err
	}
	types, err := client.CreateMetaIssueTypes(ctx, project)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Type) == "" {
		return &FieldsResult{Project: project, IssueTypes: types}, nil
	}
	issueType, err := resolveIssueType(types, in.Type)
	if err != nil {
		return nil, err
	}
	metas, err := client.CreateMetaFields(ctx, project, issueType.ID)
	if err != nil {
		return nil, err
	}
	return &FieldsResult{Project: project, Type: issueType.Name, Fields: metas}, nil
}

func (r *FieldsResult) Markdown() (string, error) {
	var b strings.Builder
	if r.Fields == nil && r.IssueTypes != nil {
		fmt.Fprintf(&b, "Issue types in %s (give one as type for its fields):\n\n", r.Project)
		for _, t := range r.IssueTypes {
			kind := ""
			if t.Subtask {
				kind = " (subtask)"
			}
			fmt.Fprintf(&b, "- %s%s\n", t.Name, kind)
		}
		return b.String(), nil
	}
	if r.Issue != "" {
		fmt.Fprintf(&b, "Fields %s can edit:\n\n", r.Issue)
	} else {
		fmt.Fprintf(&b, "Fields for creating a %s %s:\n\n", r.Project, r.Type)
	}
	b.WriteString("| Name | ID | Type | Required | Allowed values |\n| --- | --- | --- | --- | --- |\n")
	for _, meta := range r.Fields {
		required := "no"
		switch {
		case meta.Required && meta.HasDefaultValue:
			required = "yes (has default)"
		case meta.Required:
			required = "yes"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", cell(meta.Name), meta.ID(), cell(schemaType(meta.Schema)), required, cell(allowedLabels(meta, fieldsAllowedValues)))
	}
	return b.String(), nil
}

// schemaType describes a field's type, e.g. "array of option (multiselect)".
func schemaType(s jiraapi.FieldSchema) string {
	kind := s.Type
	if s.Type == "array" && s.Items != "" {
		kind = "array of " + s.Items
	}
	if _, custom, ok := strings.Cut(s.Custom, ":"); ok {
		kind += " (" + custom + ")"
	}
	return kind
}
