package jira

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/jira/adf"
)

// ResolveError reports a name that matched nothing (Candidates lists the valid
// names) or more than one thing (Ambiguous, Candidates lists the matches).
type ResolveError struct {
	Kind       string
	Name       string
	Ambiguous  bool
	Candidates []string
}

func (e *ResolveError) Error() string {
	if e.Ambiguous {
		return fmt.Sprintf("jira: %s %q is ambiguous; matches: %s", e.Kind, e.Name, strings.Join(e.Candidates, ", "))
	}
	if len(e.Candidates) == 0 {
		return fmt.Sprintf("jira: no %s %q", e.Kind, e.Name)
	}
	return fmt.Sprintf("jira: no %s %q; valid: %s", e.Kind, e.Name, strings.Join(e.Candidates, ", "))
}

// pick runs match tiers in order and returns the first tier with exactly one
// distinct result; a tier with several distinct results is ambiguous.
func pick[T any](kind, name string, items []T, id func(T) string, label func(T) string, valid []string, tiers ...func(T) bool) (T, error) {
	var zero T
	for _, match := range tiers {
		var hits []T
		seen := map[string]bool{}
		for _, it := range items {
			if match(it) && !seen[id(it)] {
				seen[id(it)] = true
				hits = append(hits, it)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		default:
			names := make([]string, len(hits))
			for i, h := range hits {
				names[i] = label(h)
			}
			return zero, &ResolveError{Kind: kind, Name: name, Ambiguous: true, Candidates: names}
		}
	}
	sort.Strings(valid)
	return zero, &ResolveError{Kind: kind, Name: name, Candidates: valid}
}

// ResolveField finds a field in create or edit metadata by ID or display
// name. An exact name wins over a case-insensitive one.
func ResolveField(fields []FieldMeta, name string) (FieldMeta, error) {
	name = strings.TrimSpace(name)
	valid := make([]string, len(fields))
	for i, f := range fields {
		valid[i] = f.Name
	}
	return pick("field", name, fields, FieldMeta.ID,
		func(f FieldMeta) string { return fmt.Sprintf("%s (%s)", f.Name, f.ID()) },
		valid,
		func(f FieldMeta) bool { return f.ID() == name || f.Key == name },
		func(f FieldMeta) bool { return f.Name == name },
		func(f FieldMeta) bool { return strings.EqualFold(f.Name, name) },
	)
}

var duplicateSuffix = regexp.MustCompile(`\s*\(\d+\)$`)

func baseName(s string) string {
	return strings.ToLower(strings.TrimSpace(duplicateSuffix.ReplaceAllString(strings.TrimSpace(s), "")))
}

// ResolveTransition finds a transition by ID, name or target status name,
// case-insensitively. Jira's " (2)" suffix on duplicate names is ignored as a
// last resort, so "Resolved" finds "Resolved (2)".
func ResolveTransition(transitions []Transition, name string) (Transition, error) {
	name = strings.TrimSpace(name)
	valid := make([]string, len(transitions))
	for i, t := range transitions {
		valid[i] = transitionLabel(t)
	}
	return pick("transition", name, transitions,
		func(t Transition) string { return t.ID },
		transitionLabel,
		valid,
		func(t Transition) bool { return t.ID == name },
		func(t Transition) bool { return strings.EqualFold(t.Name, name) },
		func(t Transition) bool { return strings.EqualFold(t.To.Name, name) },
		func(t Transition) bool {
			return baseName(t.Name) == baseName(name) || baseName(t.To.Name) == baseName(name)
		},
	)
}

func transitionLabel(t Transition) string {
	if t.To.Name == "" || strings.EqualFold(t.To.Name, t.Name) {
		return t.Name
	}
	return t.Name + " (to " + t.To.Name + ")"
}

var numericPrefix = regexp.MustCompile(`^\d+[\s.:)\-]*`)

func linkBaseName(s string) string {
	return strings.ToLower(strings.TrimSpace(numericPrefix.ReplaceAllString(strings.TrimSpace(s), "")))
}

// ResolveLinkType finds an issue link type by ID or name, case-insensitively
// and ignoring a leading number ("Relates" finds "2 Relates").
func ResolveLinkType(types []IssueLinkType, name string) (IssueLinkType, error) {
	name = strings.TrimSpace(name)
	valid := make([]string, len(types))
	for i, t := range types {
		valid[i] = t.Name
	}
	return pick("link type", name, types,
		func(t IssueLinkType) string { return t.ID },
		func(t IssueLinkType) string { return t.Name },
		valid,
		func(t IssueLinkType) bool { return t.ID == name },
		func(t IssueLinkType) bool { return strings.EqualFold(t.Name, name) },
		func(t IssueLinkType) bool { return linkBaseName(t.Name) == linkBaseName(name) },
	)
}

// ShapeFieldValue turns a user-supplied value into the JSON shape Jira expects
// for the field's schema:
//
//   - option → {"value": v}; array of options → [{"value": v}, ...]
//   - user → {"accountId": v}; array of users → [{"accountId": v}, ...]
//   - priority, version, component, resolution → {"name": v} (arrays alike)
//   - parent / issuelink → {"key": v}; project → {"key": v}
//   - number → float64 (strings are parsed)
//   - rich text (description, environment, textarea custom fields) →
//     ADF from markdown
//   - other strings, dates and arrays of strings pass through
//
// Option and named values are checked against AllowedValues when present
// and replaced by their canonical spelling. A map value is assumed to be
// shaped already. Users must already be account IDs. A nil value returns nil,
// which encodes as JSON null and clears the field; a nil inside a list is an
// error.
func ShapeFieldValue(field FieldMeta, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if m, ok := value.(map[string]any); ok {
		return m, nil
	}
	s := field.Schema
	if s.Type == "array" {
		items, err := toList(value)
		if err != nil {
			return nil, fieldErr(field, err)
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			if it == nil {
				return nil, fieldErr(field, fmt.Errorf("null in a list; pass null for the whole field to clear it"))
			}
			v, err := shapeScalar(field, s.Items, it)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	if list, ok := value.([]any); ok {
		if len(list) != 1 {
			return nil, fieldErr(field, fmt.Errorf("takes one value, got %d", len(list)))
		}
		value = list[0]
	}
	if list, ok := value.([]string); ok {
		if len(list) != 1 {
			return nil, fieldErr(field, fmt.Errorf("takes one value, got %d", len(list)))
		}
		value = list[0]
	}
	if value == nil {
		return nil, nil
	}
	return shapeScalar(field, s.Type, value)
}

func shapeScalar(field FieldMeta, kind string, value any) (any, error) {
	if m, ok := value.(map[string]any); ok {
		return m, nil
	}
	switch kind {
	case "option", "option-with-child":
		v, err := allowed(field, value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"value": v}, nil
	case "priority", "version", "component", "resolution", "securitylevel":
		v, err := allowed(field, value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"name": v}, nil
	case "user":
		return map[string]any{"accountId": fmt.Sprint(value)}, nil
	case "issuelink", "project":
		return map[string]any{"key": fmt.Sprint(value)}, nil
	case "issuetype":
		id := fmt.Sprint(value)
		if _, err := strconv.Atoi(id); err == nil {
			return map[string]any{"id": id}, nil
		}
		return map[string]any{"name": id}, nil
	case "number":
		switch n := value.(type) {
		case float64, int, int64:
			return n, nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil {
				return nil, fieldErr(field, fmt.Errorf("%q is not a number", n))
			}
			return f, nil
		}
		return nil, fieldErr(field, fmt.Errorf("%v is not a number", value))
	case "string":
		text, ok := value.(string)
		if !ok {
			return value, nil
		}
		if field.Schema.Type == "string" && IsRichText(field.Schema) {
			doc, err := adf.FromMarkdown(text)
			if err != nil {
				return nil, fieldErr(field, err)
			}
			return doc, nil
		}
		return text, nil
	}
	return value, nil
}

// IsRichText says a field holds an ADF document in API v3: description,
// environment, and multi-line text (textarea) custom fields.
func IsRichText(s FieldSchema) bool {
	switch s.System {
	case "description", "environment":
		return true
	}
	return s.Custom == "com.atlassian.jira.plugin.system.customfieldtypes:textarea"
}

// allowed returns the canonical spelling of value from field.AllowedValues,
// or value itself when Jira gave no allowed values.
func allowed(field FieldMeta, value any) (string, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if len(field.AllowedValues) == 0 {
		return text, nil
	}
	labels := make([]string, 0, len(field.AllowedValues))
	for _, av := range field.AllowedValues {
		if av.Disabled {
			continue
		}
		if av.Label() == text {
			return av.Label(), nil
		}
		labels = append(labels, av.Label())
	}
	var hits []string
	for _, l := range labels {
		if strings.EqualFold(l, text) {
			hits = append(hits, l)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		sort.Strings(labels)
		return "", &ResolveError{Kind: "value for " + field.Name, Name: text, Candidates: labels}
	}
	return "", &ResolveError{Kind: "value for " + field.Name, Name: text, Ambiguous: true, Candidates: hits}
}

func toList(value any) ([]any, error) {
	switch v := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out, nil
	case string, float64, int, int64, bool:
		return []any{v}, nil
	}
	return nil, fmt.Errorf("unsupported value %T", value)
}

func fieldErr(field FieldMeta, err error) error {
	return fmt.Errorf("jira: field %s (%s): %w", field.Name, field.ID(), err)
}
