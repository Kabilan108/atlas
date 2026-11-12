package snippet

import (
	"context"
	"encoding/json"
	"fmt"
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

const (
	defaultListLimit = 10
	maxListLimit     = 100
	// maxScanned bounds the snippets read while filtering by visibility,
	// which Bitbucket cannot do itself.
	maxScanned = 1000
)

// ListInput is the input to snippet_list.
type ListInput struct {
	Workspace  string     `json:"workspace,omitempty" jsonschema:"default: the default workspace"`
	Role       Role       `json:"role,omitempty" jsonschema:"only snippets where you have this role"`
	Visibility Visibility `json:"visibility,omitempty" jsonschema:"only public or only private snippets"`
	Limit      int        `json:"limit,omitempty" jsonschema:"default 10, max 100"`
}

// ListResult is the snippets found. It marshals to a plain array, as
// `atlas snippet list --json` always printed; the markdown says when more
// exist or the visibility filter stopped scanning.
type ListResult struct {
	Snippets []bitbucket.Snippet

	more       bool
	scanCapped bool
	limit      int
}

// MarshalJSON writes the snippets as a plain array.
func (r ListResult) MarshalJSON() ([]byte, error) {
	if r.Snippets == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(r.Snippets)
}

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
		return ListResult{}, err
	}
	limit := in.Limit
	switch {
	case limit == 0:
		limit = defaultListLimit
	case limit < 0:
		return ListResult{}, ops.Errorf(ops.Invalid, "limit must be greater than 0")
	case limit > maxListLimit:
		return ListResult{}, ops.Errorf(ops.Invalid, "limit %d is over the maximum of %d", limit, maxListLimit)
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return ListResult{}, err
	}
	opts := &bitbucket.SnippetListOptions{Limit: limit, Role: string(in.Role)}
	if in.Visibility != "" {
		private := in.Visibility == "private"
		opts.Private, opts.MaxScan = &private, maxScanned
	}
	snippets, scan, err := client.ListSnippets(ctx, workspace, opts)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Snippets: snippets, more: scan.More && !scan.Capped, scanCapped: scan.Capped, limit: limit}, nil
}

// Markdown renders the snippets as a table.
func (r ListResult) Markdown() (string, error) {
	if len(r.Snippets) == 0 {
		return "No snippets found.\n" + r.Note(), nil
	}
	rows := make([][]string, 0, len(r.Snippets))
	for _, snippet := range r.Snippets {
		rows = append(rows, []string{
			snippet.ID,
			snippet.Title,
			strconv.Itoa(len(snippet.Files)),
			VisibilityOf(snippet),
			snippet.UpdatedOn.Format("2006-01-02"),
		})
	}
	return output.MarkdownTable([]string{"ID", "Title", "Files", "Visibility", "Updated"}, rows) + r.Note(), nil
}

// Note says when more snippets exist or the visibility filter stopped
// scanning, or is empty.
func (r ListResult) Note() string {
	switch {
	case r.scanCapped:
		return fmt.Sprintf("\nChecked only the first %d snippets for visibility; filter by role to narrow the list.\n", maxScanned)
	case r.more:
		return fmt.Sprintf("\nShowing the first %d; raise limit (max %d) for more.\n", r.limit, maxListLimit)
	}
	return ""
}

// VisibilityOf names who can see snippet: "private" or "public".
func VisibilityOf(snippet bitbucket.Snippet) string {
	if snippet.IsPrivate {
		return "private"
	}
	return "public"
}
