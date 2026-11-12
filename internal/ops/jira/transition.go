package jira

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// TransitionsResult is the transitions an issue allows now.
type TransitionsResult struct {
	Key         string               `json:"key"`
	Transitions []jiraapi.Transition `json:"transitions"`
}

var TransitionList = &ops.Op[IssueRef, *TransitionsResult]{
	Meta: ops.Meta{
		Name:        "jira_transition_list",
		Description: "List the transitions a Jira issue allows now: name, target status, and the fields each one requires.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run: func(ctx context.Context, env ops.Env, in IssueRef) (*TransitionsResult, error) {
		key, err := in.Key()
		if err != nil {
			return nil, err
		}
		client, err := env.Jira(ctx)
		if err != nil {
			return nil, err
		}
		transitions, err := client.ListTransitions(ctx, key)
		if err != nil {
			return nil, err
		}
		return &TransitionsResult{Key: key, Transitions: transitions}, nil
	},
	Markdown: (*TransitionsResult).Markdown,
}

func (r *TransitionsResult) Markdown() (string, error) {
	if len(r.Transitions) == 0 {
		return fmt.Sprintf("%s has no transitions available to you.\n", r.Key), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Transitions for %s:\n\n| Transition | To status | ID | Required fields |\n| --- | --- | --- | --- |\n", r.Key)
	for _, t := range r.Transitions {
		var required []string
		for _, meta := range metaList(t.Fields) {
			if !meta.Required || meta.HasDefaultValue {
				continue
			}
			field := meta.Name
			if values := allowedLabels(meta, 10); values != "" {
				field += " (" + values + ")"
			}
			required = append(required, field)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(t.Name), cell(t.To.Name), t.ID, cell(strings.Join(required, "; ")))
	}
	return b.String(), nil
}

// TransitionInput is the input to jira_transition.
type TransitionInput struct {
	IssueRef
	Transition string         `json:"transition" jsonschema:"transition name or target status, any case (Resolved finds Resolved (2))"`
	Fields     map[string]any `json:"fields,omitempty" jsonschema:"transition screen fields by name, e.g. {\"Resolution\": \"Done\"}"`
	Force      bool           `json:"force,omitempty" jsonschema:"replace or clear a rich-text screen field even if content is lost"`
}

// Transitioned is the receipt for a transition.
type Transitioned struct {
	Key        string `json:"key"`
	Transition string `json:"transition"`
	Status     string `json:"status"`
	URL        string `json:"url"`
}

var Transition = &ops.Op[TransitionInput, *Transitioned]{
	Meta: ops.Meta{
		Name: "jira_transition",
		Description: "Move a Jira issue to a new status by transition name or target status, e.g. Resolved. Call it directly: " +
			"if the name matches nothing or a required field is missing, the error lists the valid names and fields.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run: transition,
}

func transition(ctx context.Context, env ops.Env, in TransitionInput) (*Transitioned, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Transition) == "" {
		return nil, ops.Errorf(ops.Invalid, "transition is required: give a transition name or target status")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	transitions, err := client.ListTransitions(ctx, key)
	if err != nil {
		return nil, err
	}
	chosen, err := jiraapi.ResolveTransition(transitions, in.Transition)
	if err != nil {
		return nil, invalid(fmt.Errorf("%s: %w", key, err))
	}
	metas := metaList(chosen.Fields)
	fields := map[string]any{}
	if err := shapeFields(ctx, client, screen{metas: metas}, in.Fields, fields, jiraapi.AssignableSearch{IssueKey: key}); err != nil {
		return nil, err
	}
	var richText []jiraapi.FieldMeta
	for _, id := range slices.Sorted(maps.Keys(fields)) {
		if meta := fieldMeta(metas, id); jiraapi.IsRichText(meta.Schema) {
			richText = append(richText, meta)
		}
	}
	if in.Force && len(richText) == 0 {
		return nil, ops.Errorf(ops.Invalid, "force applies only when a transition sets a rich-text field")
	}
	if !in.Force {
		if err := guardRichText(ctx, client, key, richText, fields); err != nil {
			return nil, err
		}
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := client.DoTransition(ctx, key, chosen.ID, jiraapi.TransitionInput{Fields: fields}); err != nil {
		return nil, requiredFieldsNote(withFieldNames(err, metas), chosen, metas, fields)
	}
	return &Transitioned{Key: key, Transition: chosen.Name, Status: chosen.To.Name, URL: jiraapi.BrowseURL(site, key)}, nil
}

// requiredFieldsNote adds the transition's required fields that the call
// left out to a rejected transition, with their allowed values, so the
// caller can retry with them in fields.
func requiredFieldsNote(err error, chosen jiraapi.Transition, metas []jiraapi.FieldMeta, set map[string]any) error {
	var apiErr *jiraapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return err
	}
	var missing []string
	for _, meta := range metas {
		if _, ok := set[meta.ID()]; ok || !meta.Required || meta.HasDefaultValue {
			continue
		}
		field := meta.Name
		if values := allowedLabels(meta, 10); values != "" {
			field += " (allowed: " + values + ")"
		}
		missing = append(missing, field)
	}
	if len(missing) == 0 {
		return err
	}
	return fmt.Errorf("%w [%s requires %s; pass them in fields]", err, chosen.Name, strings.Join(missing, "; "))
}
