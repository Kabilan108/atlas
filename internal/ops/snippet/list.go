package snippet

import (
	"context"
	"strconv"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

// Role filters snippets by the caller's relation to them.
type Role string

func (Role) Enum() []string { return []string{"owner", "contributor", "member"} }

// Visibility filters snippets by who can see them.
type Visibility string

func (Visibility) Enum() []string { return []string{"public", "private"} }

const defaultListLimit = 10

// ListInput is the input to snippet_list.
type ListInput struct {
	Workspace  string     `json:"workspace,omitempty" jsonschema:"default: the default workspace"`
	Role       Role       `json:"role,omitempty" jsonschema:"only snippets where you have this role"`
	Visibility Visibility `json:"visibility,omitempty" jsonschema:"only public or only private snippets"`
	Limit      int        `json:"limit,omitempty" jsonschema:"default 10"`
}

// ListResult is the snippets found. It marshals to a plain array, as
// `atlas snippet list --json` always printed.
type ListResult []bitbucket.Snippet

var List = &ops.Op[ListInput, ListResult]{
	Meta: ops.Meta{
		Name:        "snippet_list",
		Description: "List Bitbucket snippets in a workspace: ID, title, file count, visibility, last update.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      list,
	Markdown: ListResult.Markdown,
}

func list(ctx context.Context, env ops.Env, in ListInput) (ListResult, error) {
	workspace, err := ops.Workspace(in.Workspace, env)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	switch {
	case limit == 0:
		limit = defaultListLimit
	case limit < 0:
		return nil, ops.Errorf(ops.Invalid, "limit must be greater than 0")
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	// Bitbucket cannot filter by visibility, so a filtered list reads every
	// page before applying the limit.
	fetchLimit := limit
	if in.Visibility != "" {
		fetchLimit = 0
	}
	snippets, err := client.ListSnippets(ctx, workspace, &bitbucket.SnippetListOptions{Limit: fetchLimit, Role: string(in.Role)})
	if err != nil {
		return nil, err
	}
	if in.Visibility != "" {
		filtered := make([]bitbucket.Snippet, 0, len(snippets))
		for _, snippet := range snippets {
			if snippet.IsPrivate == (in.Visibility == "private") {
				filtered = append(filtered, snippet)
			}
		}
		snippets = filtered
	}
	if len(snippets) > limit {
		snippets = snippets[:limit]
	}
	return snippets, nil
}

// Markdown renders the snippets as a table.
func (r ListResult) Markdown() (string, error) {
	if len(r) == 0 {
		return "No snippets found.\n", nil
	}
	rows := make([][]string, 0, len(r))
	for _, snippet := range r {
		rows = append(rows, []string{
			snippet.ID,
			snippet.Title,
			strconv.Itoa(len(snippet.Files)),
			VisibilityOf(snippet),
			snippet.UpdatedOn.Format("2006-01-02"),
		})
	}
	return output.MarkdownTable([]string{"ID", "Title", "Files", "Visibility", "Updated"}, rows), nil
}

// VisibilityOf names who can see snippet: "private" or "public".
func VisibilityOf(snippet bitbucket.Snippet) string {
	if snippet.IsPrivate {
		return "private"
	}
	return "public"
}
