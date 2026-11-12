package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/ops"
)

// ViewInput is the input to pipeline_view.
type ViewInput struct {
	ops.RepoRef
	Builds []string `json:"builds" jsonschema:"build numbers (2090 or #2090) or pipeline UUIDs, at most 10"`
}

// ViewResult holds one entry per requested build, in order.
type ViewResult struct {
	Repo    string      `json:"repo"`
	Results []BuildView `json:"results"`
}

// BuildView is a run and its steps, or the error reading them.
type BuildView struct {
	Index  int    `json:"index"`
	Build  string `json:"build"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Run    *Run   `json:"run,omitempty"`
	Steps  []Step `json:"steps,omitempty"`
}

var View = &ops.Op[ViewInput, *ViewResult]{
	Meta: ops.Meta{
		Name:        "pipeline_view",
		Description: "Show one or more Bitbucket Pipelines runs by build number or UUID, with each step's status, start time, and duration.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      view,
	Markdown: (*ViewResult).Markdown,
}

func view(ctx context.Context, env ops.Env, in ViewInput) (*ViewResult, error) {
	builds, err := parseBuilds(in.Builds)
	if err != nil {
		return nil, err
	}
	repo, err := in.RepoRef.Resolve(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	result := &ViewResult{Repo: repo.String(), Results: make([]BuildView, len(builds))}
	err = eachBuild(builds, func(i int, build string) error {
		entry := &result.Results[i]
		entry.Index, entry.Build, entry.Status = i, build, entryOK
		fail := func(err error) error {
			entry.Status, entry.Error = entryFailed, errorText(err)
			return err
		}
		pipeline, err := client.GetPipeline(ctx, repo.Workspace, repo.Repo, build)
		if err != nil {
			return fail(err)
		}
		run := newRun(repo, pipeline)
		entry.Run = &run
		steps, err := client.ListPipelineSteps(ctx, repo.Workspace, repo.Repo, pipeline.UUID)
		if err != nil {
			return fail(fmt.Errorf("steps: %w", err))
		}
		entry.Steps = make([]Step, 0, len(steps))
		for _, step := range steps {
			entry.Steps = append(entry.Steps, newStep(step))
		}
		return nil
	})
	return result, err
}

func (r *ViewResult) Markdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pipelines in %s\n", r.Repo)
	for _, entry := range r.Results {
		b.WriteString("\n")
		if entry.Run == nil {
			fmt.Fprintf(&b, "## %s\n\nFailed: %s\n", entry.Build, entry.Error)
			continue
		}
		describeRun(&b, *entry.Run)
		if entry.Error != "" {
			fmt.Fprintf(&b, "\nFailed: %s\n", entry.Error)
			continue
		}
		if len(entry.Steps) == 0 {
			b.WriteString("\nNo steps yet.\n")
			continue
		}
		b.WriteString("\n| Step | Status | Started | Duration |\n|---|---|---|---|\n")
		for _, step := range entry.Steps {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(stepName(step)), step.Status,
				formatTime(step.StartedOn), stepDuration(step))
		}
	}
	return b.String(), nil
}

func stepName(step Step) string {
	if step.Name != "" {
		return step.Name
	}
	return step.UUID
}

func stepDuration(step Step) string {
	if step.StartedOn == nil {
		return "-"
	}
	return formatDuration(step.DurationSeconds, step.CompletedOn != nil)
}
