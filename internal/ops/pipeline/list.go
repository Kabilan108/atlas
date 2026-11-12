package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// ListInput is the input to pipeline_list.
type ListInput struct {
	ops.RepoRef
	Branch   string `json:"branch,omitempty" jsonschema:"only runs on this branch"`
	Commit   string `json:"commit,omitempty" jsonschema:"full commit SHA"`
	Pipeline string `json:"pipeline,omitempty" jsonschema:"only runs of this custom pipeline or selector pattern, e.g. db-backup"`
	Limit    int    `json:"limit,omitempty" jsonschema:"default 20, max 100"`
}

// ListResult is the newest pipeline runs matching the filters.
type ListResult struct {
	Repo      string `json:"repo"`
	Pipelines []Run  `json:"pipelines"`

	filters string
}

var List = &ops.Op[ListInput, *ListResult]{
	Meta: ops.Meta{
		Name:        "pipeline_list",
		Description: "Newest Bitbucket Pipelines runs in a repo: build number, status, branch, commit, selector, trigger, creator, start, duration. Filter by branch, commit, or custom pipeline pattern.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      list,
	Markdown: (*ListResult).Markdown,
}

func list(ctx context.Context, env ops.Env, in ListInput) (*ListResult, error) {
	limit := in.Limit
	switch {
	case limit < 0:
		return nil, ops.Errorf(ops.Invalid, "limit must not be negative")
	case limit == 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		return nil, ops.Errorf(ops.Invalid, "limit %d is over the maximum of %d", limit, maxListLimit)
	}
	repo, err := in.RepoRef.Resolve(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	opts := &bitbucket.PipelineListOptions{
		Branch:          strings.TrimSpace(in.Branch),
		Commit:          strings.TrimSpace(in.Commit),
		SelectorPattern: strings.TrimSpace(in.Pipeline),
		Limit:           limit,
	}
	pipelines, err := client.ListPipelines(ctx, repo.Workspace, repo.Repo, opts)
	if err != nil {
		return nil, err
	}
	result := &ListResult{Repo: repo.String(), Pipelines: make([]Run, 0, len(pipelines)), filters: describeFilters(opts)}
	for i := range pipelines {
		result.Pipelines = append(result.Pipelines, newRun(repo, &pipelines[i]))
	}
	return result, nil
}

func describeFilters(opts *bitbucket.PipelineListOptions) string {
	var parts []string
	for _, f := range []struct{ name, value string }{
		{"branch", opts.Branch}, {"commit", opts.Commit}, {"pattern", opts.SelectorPattern},
	} {
		if f.value != "" {
			parts = append(parts, f.name+" "+f.value)
		}
	}
	return strings.Join(parts, ", ")
}

func (r *ListResult) Markdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pipelines in %s", r.Repo)
	if r.filters != "" {
		fmt.Fprintf(&b, " (%s)", r.filters)
	}
	b.WriteString("\n\n")
	if len(r.Pipelines) == 0 {
		b.WriteString("No pipeline runs match.\n")
		return b.String(), nil
	}
	b.WriteString("| # | Status | Branch | Commit | Selector | Trigger | Creator | Started | Duration |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, run := range r.Pipelines {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			run.BuildNumber, run.Status, cell(run.Branch), cell(shortHash(run.Commit)), cell(run.Selector),
			cell(run.Trigger), cell(run.Creator), formatTime(&run.CreatedOn), formatDuration(run.DurationSeconds, run.Finished()))
	}
	fmt.Fprintf(&b, "\n%d runs, newest first. pipeline_view shows steps; pipeline_log shows failed step logs.\n", len(r.Pipelines))
	return b.String(), nil
}
