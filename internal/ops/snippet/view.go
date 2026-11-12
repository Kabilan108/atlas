package snippet

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

// ViewInput is the input to snippet_view.
type ViewInput struct {
	Target
	Filename  string `json:"filename,omitempty" jsonschema:"show only this file"`
	FilesOnly bool   `json:"files_only,omitempty" jsonschema:"list file names without contents"`
}

// ViewResult is a snippet and the contents of the files asked for. It
// marshals to the JSON `atlas snippet view --json` prints.
type ViewResult struct {
	*bitbucket.Snippet
	FileContents map[string]string `json:"file_contents,omitempty"`

	ref       Ref
	filesOnly bool
}

var View = &ops.Op[ViewInput, *ViewResult]{
	Meta: ops.Meta{
		Name:        "snippet_view",
		Description: "Show a Bitbucket snippet's files in fenced blocks, or one file with filename. files_only lists names.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      view,
	Markdown: (*ViewResult).Markdown,
}

func view(ctx context.Context, env ops.Env, in ViewInput) (*ViewResult, error) {
	ref, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	snippet, err := client.GetSnippet(ctx, ref.Workspace, ref.ID)
	if err != nil {
		return nil, err
	}
	result := &ViewResult{Snippet: snippet, ref: ref, filesOnly: in.FilesOnly}
	if in.FilesOnly {
		return result, nil
	}
	names := Filenames(snippet)
	if in.Filename != "" {
		if _, ok := snippet.Files[in.Filename]; !ok {
			return nil, ops.Errorf(ops.NotFound, "file %q not found in snippet %s", in.Filename, ref.ID)
		}
		names = []string{in.Filename}
	}
	result.FileContents = make(map[string]string, len(names))
	for _, name := range names {
		content, err := client.GetSnippetFileContent(ctx, ref.Workspace, ref.ID, name)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch file %s: %w", name, err)
		}
		result.FileContents[name] = string(content)
	}
	return result, nil
}

// Markdown renders the snippet header and each fetched file in a fence.
func (r *ViewResult) Markdown() (string, error) {
	var b strings.Builder
	title := r.Title
	if title == "" {
		title = r.ref.ID
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Snippet %s/%s, %s, updated %s", r.ref.Workspace, r.ref.ID, VisibilityOf(*r.Snippet), r.UpdatedOn.Format("2006-01-02"))
	if href := r.Links.HTML.Href; href != "" {
		fmt.Fprintf(&b, ", %s", href)
	}
	b.WriteString("\n")
	if r.filesOnly {
		b.WriteString("\n")
		for _, name := range Filenames(r.Snippet) {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		return b.String(), nil
	}
	for _, name := range sortedKeys(r.FileContents) {
		fmt.Fprintf(&b, "\n## %s\n\n", name)
		b.WriteString(output.CodeFence(r.FileContents[name], output.FenceLanguage(name)))
	}
	return b.String(), nil
}

// Filenames returns the snippet's file names in sorted order.
func Filenames(snippet *bitbucket.Snippet) []string {
	names := make([]string, 0, len(snippet.Files))
	for name := range snippet.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
