package pipeline

import (
	"context"
	"fmt"

	"github.com/kabilan108/atlas/internal/ops"
)

// StopInput is the input to pipeline_stop.
type StopInput struct {
	ops.RepoRef
	Build string `json:"build" jsonschema:"build number (2090 or #2090) or pipeline UUID"`
}

// StopReceipt reports a stop request. Bitbucket stops the run
// asynchronously; WasStatus is its status when the request was sent.
type StopReceipt struct {
	Repo        string `json:"repo"`
	BuildNumber int    `json:"build_number"`
	WasStatus   string `json:"was_status"`
	Status      string `json:"status"`
	URL         string `json:"url"`
}

var Stop = &ops.Op[StopInput, *StopReceipt]{
	Meta: ops.Meta{
		Name:        "pipeline_stop",
		Description: "Stop a running Bitbucket Pipelines run by build number. The current step is halted mid-way, so a deploy step may be left half done.",
		Surfaces:    ops.Both,
		Destructive: true,
	},
	Run:      stop,
	Markdown: (*StopReceipt).Markdown,
}

func stop(ctx context.Context, env ops.Env, in StopInput) (*StopReceipt, error) {
	build, err := parseBuild(in.Build)
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
	pipeline, err := client.GetPipeline(ctx, repo.Workspace, repo.Repo, build)
	if err != nil {
		return nil, err
	}
	if finished(pipeline) {
		return nil, ops.Errorf(ops.Conflict, "pipeline #%d already finished with %s; nothing to stop", pipeline.BuildNumber, pipeline.State.Status())
	}
	if err := client.StopPipeline(ctx, repo.Workspace, repo.Repo, pipeline.UUID); err != nil {
		return nil, err
	}
	run := newRun(repo, pipeline)
	return &StopReceipt{
		Repo:        repo.String(),
		BuildNumber: run.BuildNumber,
		WasStatus:   run.Status,
		Status:      "STOPPING",
		URL:         run.URL,
	}, nil
}

func (r *StopReceipt) Markdown() (string, error) {
	return fmt.Sprintf("Asked Bitbucket to stop pipeline #%d (was %s). It shows STOPPED once the current step halts; check with pipeline_view %d.\n%s\n",
		r.BuildNumber, r.WasStatus, r.BuildNumber, r.URL), nil
}
