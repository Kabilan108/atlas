package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/jira/adf"
	"github.com/kabilan108/atlas/internal/ops"
)

const defaultViewComments = 10

// ViewInput is the input to jira_issue_view.
type ViewInput struct {
	IssueRef
	Comments int  `json:"comments,omitempty" jsonschema:"recent comments to show (default 10, max 100)"`
	History  bool `json:"history,omitempty" jsonschema:"add the change history (newest 500 entries)"`
}

// ViewResult is an issue with the comments and history its markdown shows.
type ViewResult struct {
	Issue         *jiraapi.Issue             `json:"issue"`
	Comments      []jiraapi.Comment          `json:"comments"`
	TotalComments int                        `json:"total_comments"`
	History       []jiraapi.ChangelogHistory `json:"history,omitempty"`
	TotalHistory  int                        `json:"total_history,omitempty"`
	URL           string                     `json:"url"`

	site  string
	media map[string]jiraapi.RenderedAttachment
}

var View = &ops.Op[ViewInput, *ViewResult]{
	Meta: ops.Meta{
		Name: "jira_issue_view",
		Description: "Show a Jira issue as markdown: status, people, parent, labels, links, attachments, custom fields by name, " +
			"description and recent comments (marked internal where they are). Set history for the changelog.",
		Surfaces: ops.Both,
		ReadOnly: true,
	},
	Run:      view,
	Markdown: (*ViewResult).Markdown,
}

func view(ctx context.Context, env ops.Env, in ViewInput) (*ViewResult, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	issue, err := client.GetIssue(ctx, key, jiraapi.GetIssueOptions{
		Fields: []string{"*all", "-comment", "-worklog"},
		Expand: []string{"names", "schema", "renderedFields"},
	})
	if err != nil {
		return nil, err
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	result := &ViewResult{Issue: issue, URL: jiraapi.BrowseURL(site, issue.Key), site: site, media: map[string]jiraapi.RenderedAttachment{}}
	for _, rendered := range issue.RenderedFields {
		var html string
		if json.Unmarshal(rendered, &html) == nil {
			maps.Copy(result.media, jiraapi.RenderedMedia(html))
		}
	}
	limit, err := boundedCount("comments", in.Comments, defaultViewComments, MaxViewComments)
	if err != nil {
		return nil, err
	}
	if result.Comments, result.TotalComments, err = recentComments(ctx, client, issue.Key, limit); err != nil {
		return nil, fmt.Errorf("fetch comments: %w", err)
	}
	for _, comment := range result.Comments {
		maps.Copy(result.media, jiraapi.RenderedMedia(comment.RenderedBody))
	}
	if in.History {
		if result.History, result.TotalHistory, err = client.ChangelogNewest(ctx, issue.Key, MaxHistory); err != nil {
			return nil, fmt.Errorf("fetch history: %w", err)
		}
	}
	return result, nil
}

// recentComments returns the newest limit comments, oldest first, and the
// issue's comment count, reading no more than limit.
func recentComments(ctx context.Context, client *jiraapi.Client, key string, limit int) ([]jiraapi.Comment, int, error) {
	opts := jiraapi.CommentListOptions{OrderBy: "-created", Expand: []string{"renderedBody"}}
	var comments []jiraapi.Comment
	total := 0
	for start := 0; ; {
		size := min(100, limit-len(comments))
		page, err := client.ListCommentsPage(ctx, key, start, size, opts)
		if err != nil {
			return nil, 0, err
		}
		total = page.Total
		comments = append(comments, page.Comments...)
		start += len(page.Comments)
		if len(page.Comments) == 0 || start >= total || len(comments) >= limit {
			break
		}
	}
	slices.Reverse(comments)
	return comments, max(total, len(comments)), nil
}

// Markdown renders the issue without calling Jira.
func (r *ViewResult) Markdown() (string, error) {
	issue, f := r.Issue, r.Issue.Fields
	approx := adf.Approximations{}
	render := func(raw json.RawMessage) (string, error) {
		found, err := adf.Approximate(raw)
		if err != nil {
			return "", err
		}
		approx.Add(found)
		return adf.ToMarkdown(raw, adf.WithMediaLink(r.mediaLink))
	}

	var body bytes.Buffer
	fields, err := r.customFields(render)
	if err != nil {
		return "", err
	}
	if len(fields) > 0 {
		body.WriteString("\n## Fields\n\n")
		var long []string
		for _, field := range fields {
			if strings.Contains(field.value, "\n") {
				long = append(long, fmt.Sprintf("### %s\n\n%s\n", field.name, field.value))
				continue
			}
			fmt.Fprintf(&body, "- **%s:** %s\n", field.name, field.value)
		}
		for _, section := range long {
			body.WriteString("\n" + section)
		}
	}
	description, err := render(f.Description)
	if err != nil {
		return "", fmt.Errorf("render description: %w", err)
	}
	body.WriteString("\n## Description\n\n")
	if strings.TrimSpace(description) == "" {
		body.WriteString("_No description._\n")
	} else {
		body.WriteString(strings.TrimRight(description, "\n") + "\n")
	}
	r.writeLinks(&body)
	r.writeAttachments(&body)
	if err := r.writeComments(&body, render); err != nil {
		return "", err
	}
	r.writeHistory(&body)

	var out bytes.Buffer
	fmt.Fprintf(&out, "# %s: %s\n\n%s\n", issue.Key, f.Summary, r.URL)
	if approx.Total() > 0 {
		fmt.Fprintf(&out, "\n> Shown approximately: %s. Open the issue in Jira for those.\n", approx)
	}
	out.WriteString("\n")
	for _, line := range r.summaryLines() {
		out.WriteString("- " + line + "\n")
	}
	out.Write(body.Bytes())
	return out.String(), nil
}

func (r *ViewResult) summaryLines() []string {
	f := r.Issue.Fields
	var lines []string
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf("**%s:** %s", label, value))
		}
	}
	if f.Status != nil {
		add("Status", f.Status.Name)
	}
	if f.IssueType != nil {
		add("Type", f.IssueType.Name)
	}
	if f.Priority != nil {
		add("Priority", f.Priority.Name)
	}
	if f.Project != nil {
		add("Project", fmt.Sprintf("%s (%s)", f.Project.Name, f.Project.Key))
	}
	assignee := userName(f.Assignee)
	if assignee == "" {
		assignee = "Unassigned"
	}
	add("Assignee", assignee)
	add("Reporter", userName(f.Reporter))
	if f.Parent != nil {
		add("Parent", strings.TrimSpace(f.Parent.Key+" "+f.Parent.Fields.Summary))
	}
	add("Labels", strings.Join(f.Labels, ", "))
	add("Components", namedRefs(f.Components))
	add("Fix versions", namedRefs(f.FixVersions))
	if f.Resolution != nil {
		add("Resolution", strings.TrimSpace(f.Resolution.Name+" "+parenthesized(when(f.ResolutionDate))))
	}
	add("Due", f.DueDate)
	add("Created", when(f.Created))
	add("Updated", when(f.Updated))
	var tracking struct {
		TimeSpent         string `json:"timeSpent"`
		RemainingEstimate string `json:"remainingEstimate"`
	}
	if raw, ok := r.Issue.RawFields["timetracking"]; ok && json.Unmarshal(raw, &tracking) == nil {
		add("Time spent", tracking.TimeSpent)
	}
	return lines
}

func parenthesized(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

func namedRefs(refs []jiraapi.NamedRef) string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name
	}
	return strings.Join(names, ", ")
}

type namedValue struct {
	name, value string
}

// customFields renders every custom field that has a value people read,
// by display name. Internal plumbing (ranks, SLAs, development summaries)
// is skipped.
func (r *ViewResult) customFields(render func(json.RawMessage) (string, error)) ([]namedValue, error) {
	var out []namedValue
	for id, raw := range r.Issue.RawFields {
		if !strings.HasPrefix(id, "customfield_") {
			continue
		}
		schema := r.Issue.Schema[id]
		if schema.Type == "any" || strings.HasPrefix(schema.Type, "sd-servicelevelagreement") || strings.HasSuffix(schema.Custom, ":gh-lexo-rank") {
			continue
		}
		value, err := fieldText(raw, render)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", id, err)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		name := r.Issue.Names[id]
		if name == "" {
			name = id
		}
		out = append(out, namedValue{name, value})
	}
	slices.SortFunc(out, func(a, b namedValue) int { return strings.Compare(a.name, b.name) })
	return out, nil
}

// fieldText renders a field value of any common shape as text.
func fieldText(raw json.RawMessage, render func(json.RawMessage) (string, error)) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	switch value := v.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case float64, bool:
		return fmt.Sprint(value), nil
	case []any:
		var parts []string
		for _, item := range value {
			data, _ := json.Marshal(item)
			text, err := fieldText(data, render)
			if err != nil {
				return "", err
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", "), nil
	case map[string]any:
		if value["type"] == "doc" {
			text, err := render(raw)
			return strings.TrimSpace(text), err
		}
		if inner, ok := value["requestType"].(map[string]any); ok {
			name, _ := inner["name"].(string)
			return name, nil
		}
		for _, key := range []string{"value", "displayName", "name", "key"} {
			if s, ok := value[key].(string); ok && s != "" {
				if child, ok := value["child"].(map[string]any); ok {
					if c, ok := child["value"].(string); ok {
						return s + " › " + c, nil
					}
				}
				return s, nil
			}
		}
	}
	return "", nil
}

// mediaLink resolves a media node to its attachment through the rendered
// HTML, then by file name; an unresolved one renders as [attachment] and
// the Attachments section lists every file.
func (r *ViewResult) mediaLink(m adf.Media) (string, string) {
	if ref, ok := r.media[m.ID]; ok {
		name := ref.Name
		for _, att := range r.Issue.Fields.Attachments {
			if att.ID == ref.AttachmentID {
				name = att.Filename
			}
		}
		return name, jiraapi.AttachmentURL(r.site, ref.AttachmentID)
	}
	if m.Alt != "" {
		for _, att := range r.Issue.Fields.Attachments {
			if att.Filename == m.Alt {
				return att.Filename, jiraapi.AttachmentURL(r.site, att.ID)
			}
		}
	}
	return m.Alt, m.URL
}

func (r *ViewResult) writeLinks(w *bytes.Buffer) {
	links := r.Issue.Fields.IssueLinks
	if len(links) == 0 {
		return
	}
	w.WriteString("\n## Links\n\n")
	for _, link := range links {
		phrase, other := link.Type.Outward, link.OutwardIssue
		if other == nil {
			phrase, other = link.Type.Inward, link.InwardIssue
		}
		if other == nil {
			continue
		}
		status := ""
		if other.Fields.Status != nil {
			status = " (" + other.Fields.Status.Name + ")"
		}
		fmt.Fprintf(w, "- %s %s: %s%s\n", phrase, other.Key, other.Fields.Summary, status)
	}
}

func (r *ViewResult) writeAttachments(w *bytes.Buffer) {
	attachments := r.Issue.Fields.Attachments
	if len(attachments) == 0 {
		return
	}
	w.WriteString("\n## Attachments\n\n")
	for _, att := range attachments {
		details := []string{byteSize(att.Size)}
		if att.MimeType != "" {
			details = append(details, att.MimeType)
		}
		if name := userName(att.Author); name != "" {
			details = append(details, name)
		}
		if att.Created != "" {
			details = append(details, when(att.Created))
		}
		fmt.Fprintf(w, "- [%s](%s) (%s)\n", escapeLinkText(att.Filename), jiraapi.AttachmentURL(r.site, att.ID), strings.Join(details, ", "))
	}
}

func escapeLinkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(s)
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (r *ViewResult) writeComments(w *bytes.Buffer, render func(json.RawMessage) (string, error)) error {
	if r.TotalComments == 0 {
		w.WriteString("\n## Comments\n\nNo comments.\n")
		return nil
	}
	if len(r.Comments) < r.TotalComments {
		fmt.Fprintf(w, "\n## Comments (newest %d of %d)\n", len(r.Comments), r.TotalComments)
	} else {
		fmt.Fprintf(w, "\n## Comments (%d)\n", r.TotalComments)
	}
	for _, comment := range r.Comments {
		heading := []string{userName(comment.Author), when(comment.Created)}
		switch {
		case comment.JSDPublic != nil && !*comment.JSDPublic:
			heading = append(heading, "internal")
		case comment.Visibility != nil:
			heading = append(heading, "restricted to "+comment.Visibility.Value)
		}
		text, err := render(comment.Body)
		if err != nil {
			return fmt.Errorf("render comment %s: %w", comment.ID, err)
		}
		fmt.Fprintf(w, "\n### %s\n\n%s\n", strings.Join(slices.DeleteFunc(heading, func(s string) bool { return s == "" }), " · "), strings.TrimRight(text, "\n"))
	}
	return nil
}

func (r *ViewResult) writeHistory(w *bytes.Buffer) {
	if r.History == nil {
		return
	}
	if r.TotalHistory > len(r.History) {
		fmt.Fprintf(w, "\n## History (newest %d of %d)\n\n", len(r.History), r.TotalHistory)
	} else {
		w.WriteString("\n## History\n\n")
	}
	if len(r.History) == 0 {
		w.WriteString("No changes.\n")
		return
	}
	for _, h := range r.History {
		changes := make([]string, 0, len(h.Items))
		for _, item := range h.Items {
			changes = append(changes, fmt.Sprintf("%s: %s → %s", item.Field, orDash(item.FromString), orDash(item.ToString)))
		}
		fmt.Fprintf(w, "- %s %s: %s\n", when(h.Created), userName(h.Author), strings.Join(changes, "; "))
	}
}

func orDash(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "—"
	}
	if runes := []rune(s); len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return s
}
