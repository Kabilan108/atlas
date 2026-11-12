package repo

import (
	"context"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const defaultRepoLimit = 100

// ListInput is the input to repo_list.
type ListInput struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"default: the default workspace"`
	Name      string `json:"name,omitempty" jsonschema:"case-insensitive substring of the repo name"`
	Limit     int    `json:"limit,omitempty" jsonschema:"default 100"`
}

// ListResult is the repositories found, most recently updated first.
type ListResult struct {
	Workspace    string                 `json:"workspace"`
	Repositories []bitbucket.Repository `json:"repositories"`
	More         bool                   `json:"more,omitempty"`

	limit int
}

var List = &ops.Op[ListInput, *ListResult]{
	Meta: ops.Meta{
		Name:        "repo_list",
		Description: "List repositories in a Bitbucket workspace, most recently updated first: slug, main branch, visibility, description.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      listRepos,
	Markdown: (*ListResult).Markdown,
}

func listRepos(ctx context.Context, env ops.Env, in ListInput) (*ListResult, error) {
	workspace, err := ops.Workspace(in.Workspace, env)
	if err != nil {
		return nil, err
	}
	limit, err := limitOrDefault(in.Limit, defaultRepoLimit)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	repos, err := client.SearchRepositories(ctx, workspace, &bitbucket.RepositoryListOptions{
		Name:  strings.TrimSpace(in.Name),
		Sort:  "-updated_on",
		Limit: limit + 1,
	})
	if err != nil {
		return nil, err
	}
	result := &ListResult{Workspace: workspace, limit: limit}
	result.Repositories, result.More = trimMore(repos, limit)
	return result, nil
}

// Markdown renders the repositories as a table.
func (r *ListResult) Markdown() (string, error) {
	if len(r.Repositories) == 0 {
		return "No repositories found in " + r.Workspace + ".\n", nil
	}
	rows := make([][]string, 0, len(r.Repositories))
	for _, repo := range r.Repositories {
		visibility := "public"
		if repo.IsPrivate {
			visibility = "private"
		}
		rows = append(rows, []string{
			Slug(repo),
			repo.MainBranch.Name,
			visibility,
			repo.UpdatedOn.Format("2006-01-02"),
			output.Truncate(firstLine(repo.Description), 60),
		})
	}
	return output.MarkdownTable([]string{"Repo", "Main branch", "Visibility", "Updated", "Description"}, rows) + moreNote(r.More, r.limit), nil
}

// Slug is the repository's URL name.
func Slug(repo bitbucket.Repository) string {
	if _, slug, ok := strings.Cut(repo.FullName, "/"); ok {
		return slug
	}
	return repo.Name
}
