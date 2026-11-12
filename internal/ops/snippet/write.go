package snippet

import (
	"context"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// CreateInput is the input to snippet_create.
type CreateInput struct {
	Workspace string            `json:"workspace,omitempty" jsonschema:"default: the default workspace"`
	Title     string            `json:"title"`
	Files     map[string]string `json:"files" jsonschema:"file name to content"`
	Public    bool              `json:"public,omitempty" jsonschema:"list publicly; default private"`
}

// Create's result is the new snippet, as `atlas snippet create --json`
// prints it.
var Create = &ops.Op[CreateInput, *bitbucket.Snippet]{
	Meta: ops.Meta{
		Name:        "snippet_create",
		Description: "Create a Bitbucket snippet from files (name to content). Private unless public is set. Returns its ID and URL.",
		Surfaces:    ops.Both,
	},
	Run: create,
	Markdown: func(r *bitbucket.Snippet) (string, error) {
		return fmt.Sprintf("Created snippet: %s\nURL: %s\n", r.ID, r.Links.HTML.Href), nil
	},
}

func create(ctx context.Context, env ops.Env, in CreateInput) (*bitbucket.Snippet, error) {
	workspace, err := ops.Workspace(in.Workspace, env)
	if err != nil {
		return nil, err
	}
	if in.Title == "" {
		return nil, ops.Errorf(ops.Invalid, "title is required")
	}
	if len(in.Files) == 0 {
		return nil, ops.Errorf(ops.Invalid, "files is required: give at least one file")
	}
	files, err := fileBytes(in.Files)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	return client.CreateSnippet(ctx, workspace, in.Title, files, !in.Public)
}

// EditInput is the input to snippet_edit.
type EditInput struct {
	Target
	Title  string            `json:"title,omitempty" jsonschema:"new title"`
	Files  map[string]string `json:"files,omitempty" jsonschema:"files to add or replace: name to content"`
	Remove []string          `json:"remove,omitempty" jsonschema:"file names to delete"`
}

// Receipt reports a snippet write.
type Receipt struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

var Edit = &ops.Op[EditInput, Receipt]{
	Meta: ops.Meta{
		Name:        "snippet_edit",
		Description: "Change a Bitbucket snippet: new title, add or replace files (name to content), remove files.",
		Surfaces:    ops.Both,
	},
	Run: edit,
	Markdown: func(r Receipt) (string, error) {
		return fmt.Sprintf("Updated snippet: %s\n", r.ID), nil
	},
}

func edit(ctx context.Context, env ops.Env, in EditInput) (Receipt, error) {
	ref, err := in.Resolve(env)
	if err != nil {
		return Receipt{}, err
	}
	if in.Title == "" && len(in.Files) == 0 && len(in.Remove) == 0 {
		return Receipt{}, ops.Errorf(ops.Invalid, "nothing to edit")
	}
	files, err := fileBytes(in.Files)
	if err != nil {
		return Receipt{}, err
	}
	for _, name := range in.Remove {
		if err := validateFilename(name); err != nil {
			return Receipt{}, err
		}
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if err := client.UpdateSnippet(ctx, ref.Workspace, ref.ID, in.Title, files, in.Remove); err != nil {
		return Receipt{}, err
	}
	return Receipt{Workspace: ref.Workspace, ID: ref.ID}, nil
}

// RenameInput is the input to snippet_rename.
type RenameInput struct {
	Target
	From string `json:"from" jsonschema:"current file name"`
	To   string `json:"to" jsonschema:"new file name"`
}

var Rename = &ops.Op[RenameInput, Receipt]{
	Meta: ops.Meta{
		Name:        "snippet_rename",
		Description: "Rename a file in a Bitbucket snippet, keeping its content.",
		Surfaces:    ops.Both,
	},
	Run: rename,
	Markdown: func(r Receipt) (string, error) {
		return fmt.Sprintf("Renamed %s to %s in snippet %s\n", r.From, r.To, r.ID), nil
	},
}

func rename(ctx context.Context, env ops.Env, in RenameInput) (Receipt, error) {
	ref, err := in.Resolve(env)
	if err != nil {
		return Receipt{}, err
	}
	for _, name := range []string{in.From, in.To} {
		if err := validateFilename(name); err != nil {
			return Receipt{}, err
		}
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return Receipt{}, err
	}
	body, err := client.GetSnippetFileContentLimit(ctx, ref.Workspace, ref.ID, in.From, maxFileBytes)
	if err != nil {
		return Receipt{}, fmt.Errorf("failed to fetch file %s: %w", in.From, err)
	}
	if body.Truncated {
		return Receipt{}, ops.Errorf(ops.Invalid, "file %s is over %d bytes, too large to rename here; rename it on bitbucket.org", in.From, maxFileBytes)
	}
	if err := client.UpdateSnippet(ctx, ref.Workspace, ref.ID, "", map[string][]byte{in.To: body.Data}, []string{in.From}); err != nil {
		return Receipt{}, err
	}
	return Receipt{Workspace: ref.Workspace, ID: ref.ID, From: in.From, To: in.To}, nil
}

// DeleteInput is the input to snippet_delete.
type DeleteInput struct {
	Target
}

var Delete = &ops.Op[DeleteInput, Receipt]{
	Meta: ops.Meta{
		Name:        "snippet_delete",
		Description: "Permanently delete a Bitbucket snippet and all its files.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run: deleteSnippet,
	Markdown: func(r Receipt) (string, error) {
		return fmt.Sprintf("Deleted snippet: %s\n", r.ID), nil
	},
}

func deleteSnippet(ctx context.Context, env ops.Env, in DeleteInput) (Receipt, error) {
	ref, err := in.Resolve(env)
	if err != nil {
		return Receipt{}, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if err := client.DeleteSnippet(ctx, ref.Workspace, ref.ID); err != nil {
		return Receipt{}, err
	}
	return Receipt{Workspace: ref.Workspace, ID: ref.ID}, nil
}

func fileBytes(files map[string]string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(files))
	for name, content := range files {
		if err := validateFilename(name); err != nil {
			return nil, err
		}
		out[name] = []byte(content)
	}
	return out, nil
}
