// Package jira holds the Jira ops (issues, search, comments, transitions,
// links, worklogs, attachments) on top of internal/jira.
package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/jira/adf"
	"github.com/kabilan108/atlas/internal/ops"
)

// IssueRef names one issue. Every op on a single issue embeds it.
type IssueRef struct {
	Issue string `json:"issue" jsonschema:"issue key, e.g. CSM-123, or browse URL"`
}

var (
	issueKeyPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)
	issueIDPattern    = regexp.MustCompile(`^[0-9]+$`)
	projectKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

// Key validates ref.Issue and returns the issue key (upper-cased) or ID.
func (ref IssueRef) Key() (string, error) {
	return issueKey("issue", ref.Issue)
}

func issueKey(field, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ops.Errorf(ops.Invalid, "%s is required: give an issue key such as CSM-123", field)
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", ops.Errorf(ops.Invalid, "invalid %s URL %q", field, raw)
		}
		if _, rest, ok := strings.Cut(u.Path, "/browse/"); ok {
			s = strings.Trim(rest, "/")
		} else if q := u.Query().Get("selectedIssue"); q != "" {
			s = q
		}
	}
	switch {
	case issueKeyPattern.MatchString(s):
		return strings.ToUpper(s), nil
	case issueIDPattern.MatchString(s):
		return s, nil
	}
	return "", ops.Errorf(ops.Invalid, "invalid %s %q: expected an issue key such as CSM-123 or a browse URL", field, raw)
}

func projectKey(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if !projectKeyPattern.MatchString(s) {
		return "", ops.Errorf(ops.Invalid, "invalid project %q: expected a project key such as CSM", raw)
	}
	return strings.ToUpper(s), nil
}

// siteURL is the browsable site behind the gateway, for links in results.
func siteURL(ctx context.Context, client *jiraapi.Client) (string, error) {
	info, err := client.ServerInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("find the Jira site URL: %w", err)
	}
	return strings.TrimRight(info.BaseURL, "/"), nil
}

// invalid makes a name-resolution failure an Invalid error, keeping its
// list of valid names.
func invalid(err error) error {
	var resolveErr *jiraapi.ResolveError
	if errors.As(err, &resolveErr) {
		return ops.Errorf(ops.Invalid, "%w", err)
	}
	if ops.KindOf(err) == ops.Unknown {
		return ops.Errorf(ops.Invalid, "%w", err)
	}
	return err
}

var accountIDPattern = regexp.MustCompile(`^([0-9a-f]{24}|[0-9]{6}:[0-9a-f-]{36}|qm:[0-9a-f-]{36}:[0-9a-f-]{36})$`)

// unassigned is the assignee value that clears the assignee.
const unassigned = "none"

// resolveUser turns "me", an account ID, an email address, or a name into
// an account. scope narrows the search to users assignable to an issue or a
// project when either is set.
func resolveUser(ctx context.Context, client *jiraapi.Client, who string, scope jiraapi.AssignableSearch) (*jiraapi.User, error) {
	who = strings.TrimSpace(who)
	switch {
	case who == "":
		return nil, ops.Errorf(ops.Invalid, "user is empty: give me, an email, a name, or an account ID")
	case strings.EqualFold(who, "me"):
		return client.Myself(ctx)
	case accountIDPattern.MatchString(who):
		return &jiraapi.User{AccountID: who}, nil
	}
	var users []jiraapi.User
	var err error
	if scope.IssueKey != "" || scope.ProjectKey != "" {
		scope.Query, scope.MaxResults = who, 20
		users, err = client.SearchAssignableUsers(ctx, scope)
	} else {
		users, err = client.SearchUsers(ctx, who, 20)
	}
	if err != nil {
		return nil, fmt.Errorf("look up user %q: %w", who, err)
	}
	users = slices.DeleteFunc(users, func(u jiraapi.User) bool { return !u.Active || u.AccountType == "app" })
	for _, match := range []func(jiraapi.User) bool{
		func(u jiraapi.User) bool { return u.EmailAddress != "" && strings.EqualFold(u.EmailAddress, who) },
		func(u jiraapi.User) bool { return strings.EqualFold(u.DisplayName, who) },
		func(jiraapi.User) bool { return true },
	} {
		var hits []jiraapi.User
		for _, u := range users {
			if match(u) {
				hits = append(hits, u)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return &hits[0], nil
		}
		names := make([]string, len(hits))
		for i, u := range hits {
			names[i] = fmt.Sprintf("%s (%s)", u.DisplayName, u.AccountID)
		}
		return nil, ops.Errorf(ops.Invalid, "%w", &jiraapi.ResolveError{Kind: "user", Name: who, Ambiguous: true, Candidates: names})
	}
	where := ""
	switch {
	case scope.IssueKey != "":
		where = " assignable to " + scope.IssueKey
	case scope.ProjectKey != "":
		where = " assignable in " + scope.ProjectKey
	}
	return nil, ops.Errorf(ops.Invalid, "no active user%s matches %q", where, who)
}

// shapeFields resolves fields given by display name or ID against screen
// metadata and shapes each value for Jira. User fields take the same values
// as an assignee. set already holds the fields the op filled in itself.
func shapeFields(ctx context.Context, client *jiraapi.Client, metas []jiraapi.FieldMeta, fields map[string]any, set map[string]any, scope jiraapi.AssignableSearch) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		meta, err := jiraapi.ResolveField(metas, name)
		if err != nil {
			return invalid(err)
		}
		id := meta.ID()
		if _, dup := set[id]; dup {
			return ops.Errorf(ops.Invalid, "field %s (%s) is set twice", meta.Name, id)
		}
		value := fields[name]
		if isUserField(meta) {
			if value, err = userValues(ctx, client, value, scope); err != nil {
				return err
			}
		}
		shaped, err := jiraapi.ShapeFieldValue(meta, value)
		if err != nil {
			return invalid(err)
		}
		set[id] = shaped
	}
	return nil
}

func isUserField(meta jiraapi.FieldMeta) bool {
	s := meta.Schema
	return s.Type == "user" || (s.Type == "array" && s.Items == "user")
}

func userValues(ctx context.Context, client *jiraapi.Client, value any, scope jiraapi.AssignableSearch) (any, error) {
	lookup := func(v any) (any, error) {
		who, ok := v.(string)
		if !ok {
			return v, nil
		}
		user, err := resolveUser(ctx, client, who, scope)
		if err != nil {
			return nil, err
		}
		return user.AccountID, nil
	}
	if list, ok := value.([]any); ok {
		out := make([]any, len(list))
		for i, v := range list {
			resolved, err := lookup(v)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	}
	return lookup(value)
}

// metaList turns a transition's field map into a list with IDs set.
func metaList(fields map[string]jiraapi.FieldMeta) []jiraapi.FieldMeta {
	list := make([]jiraapi.FieldMeta, 0, len(fields))
	for _, id := range slices.Sorted(maps.Keys(fields)) {
		meta := fields[id]
		if meta.FieldID == "" {
			meta.FieldID = id
		}
		list = append(list, meta)
	}
	return list
}

// withFieldNames adds field names and allowed values to a 400 whose errors
// Jira keyed by field ID, so the caller can fix the input by name.
func withFieldNames(err error, metas []jiraapi.FieldMeta) error {
	var apiErr *jiraapi.APIError
	if !errors.As(err, &apiErr) || len(apiErr.Errors) == 0 {
		return err
	}
	byID := map[string]jiraapi.FieldMeta{}
	for _, meta := range metas {
		byID[meta.ID()] = meta
	}
	var notes []string
	for _, id := range slices.Sorted(maps.Keys(apiErr.Errors)) {
		meta, ok := byID[id]
		if !ok {
			continue
		}
		note := id + " is " + meta.Name
		if values := allowedLabels(meta, 10); values != "" {
			note += " (allowed: " + values + ")"
		}
		notes = append(notes, note)
	}
	if len(notes) == 0 {
		return err
	}
	return fmt.Errorf("%w [%s]", err, strings.Join(notes, "; "))
}

// allowedLabels lists up to limit allowed values; limit <= 0 lists all.
func allowedLabels(meta jiraapi.FieldMeta, limit int) string {
	var labels []string
	for _, v := range meta.AllowedValues {
		if !v.Disabled {
			labels = append(labels, v.Label())
		}
	}
	if limit > 0 && len(labels) > limit {
		return strings.Join(labels[:limit], ", ") + fmt.Sprintf(", … (+%d more)", len(labels)-limit)
	}
	return strings.Join(labels, ", ")
}

// cell makes text safe for one markdown table cell.
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

// when shows a Jira timestamp in its own offset, to the minute.
func when(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := jiraapi.ParseTime(ts)
	if err != nil {
		if len(ts) >= 10 {
			return ts[:10]
		}
		return ts
	}
	return t.Format("2006-01-02 15:04 -0700")
}

func userName(u *jiraapi.User) string {
	switch {
	case u == nil:
		return ""
	case u.DisplayName != "":
		return u.DisplayName
	}
	return u.AccountID
}

// markdownBody converts a markdown body for a comment or description.
func markdownBody(field, markdown string) (json.RawMessage, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, ops.Errorf(ops.Invalid, "%s is empty", field)
	}
	if len(markdown) > MaxBodyBytes {
		return nil, ops.Errorf(ops.Invalid, "%s is %d bytes, over the %d-byte limit", field, len(markdown), MaxBodyBytes)
	}
	doc, err := adf.FromMarkdown(markdown)
	if err != nil {
		return nil, ops.Errorf(ops.Invalid, "convert %s: %w", field, err)
	}
	return doc, nil
}

// formatSeconds writes a duration the way Jira does: 1h 30m.
func formatSeconds(seconds int) string {
	d := time.Duration(seconds) * time.Second
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}
