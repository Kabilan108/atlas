package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

const maxRunWait = 600 * time.Second

// Poll intervals while waiting for a run; tests shorten them.
var (
	pollInitial = 2 * time.Second
	pollMax     = 15 * time.Second
)

// Variable is a pipeline variable for one run.
type Variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Secured bool   `json:"secured,omitempty"`
}

// RunInput is the input to pipeline_run.
type RunInput struct {
	ops.RepoRef
	Branch      string     `json:"branch,omitempty" jsonschema:"this or commit is required"`
	Commit      string     `json:"commit,omitempty" jsonschema:"full SHA; with branch, pins the run"`
	Pipeline    string     `json:"pipeline,omitempty" jsonschema:"custom pipeline name; default: the branch's pipeline"`
	Variables   []Variable `json:"variables,omitempty"`
	WaitSeconds int        `json:"wait_seconds,omitempty" jsonschema:"seconds to wait for the result, max 600; default 0"`
}

// RunReceipt reports a started run. Finished is false when the call
// returned before the run ended; the run keeps going either way.
type RunReceipt struct {
	Repo            string `json:"repo"`
	BuildNumber     int    `json:"build_number"`
	UUID            string `json:"uuid"`
	Status          string `json:"status"`
	Finished        bool   `json:"finished"`
	Selector        string `json:"selector,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Commit          string `json:"commit,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
	URL             string `json:"url"`

	waited time.Duration
}

var Start = &ops.Op[RunInput, *RunReceipt]{
	Meta: ops.Meta{
		Name:        "pipeline_run",
		Description: "Start a Bitbucket Pipelines run on a branch or commit, optionally a custom pipeline with variables. Returns the build number; never retry a started run.",
		Surfaces:    ops.Both,
	},
	Run:      run,
	Markdown: (*RunReceipt).Markdown,
}

func run(ctx context.Context, env ops.Env, in RunInput) (*RunReceipt, error) {
	if in.WaitSeconds < 0 {
		return nil, ops.Errorf(ops.Invalid, "wait_seconds must not be negative")
	}
	wait := time.Duration(in.WaitSeconds) * time.Second
	if wait > maxRunWait {
		return nil, ops.Errorf(ops.Invalid, "wait_seconds %d is over the %d-second maximum", in.WaitSeconds, int(maxRunWait.Seconds()))
	}
	if strings.TrimSpace(in.Branch) == "" && strings.TrimSpace(in.Commit) == "" {
		return nil, ops.Errorf(ops.Invalid, "give branch or commit to run on")
	}
	variables := make([]bitbucket.PipelineVariable, 0, len(in.Variables))
	for i, v := range in.Variables {
		if strings.TrimSpace(v.Key) == "" {
			return nil, ops.Errorf(ops.Invalid, "variables[%d] has no key", i)
		}
		variables = append(variables, bitbucket.PipelineVariable{Key: v.Key, Value: v.Value, Secured: v.Secured})
	}
	repo, err := in.RepoRef.Resolve(env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	started, err := client.RunPipeline(ctx, repo.Workspace, repo.Repo, bitbucket.PipelineRunInput{
		Branch:         in.Branch,
		Commit:         in.Commit,
		CustomPipeline: in.Pipeline,
		Variables:      variables,
	})
	if err != nil {
		return nil, err
	}
	receipt := newRunReceipt(repo, started)
	if wait == 0 || receipt.Finished {
		return receipt, nil
	}
	receipt.waited = wait
	return waitForRun(ctx, client, repo, receipt, time.Now().Add(wait))
}

func newRunReceipt(repo ops.Repository, p *bitbucket.Pipeline) *RunReceipt {
	run := newRun(repo, p)
	return &RunReceipt{
		Repo:            repo.String(),
		BuildNumber:     run.BuildNumber,
		UUID:            run.UUID,
		Status:          run.Status,
		Finished:        finished(p),
		Selector:        run.Selector,
		Branch:          run.Branch,
		Commit:          run.Commit,
		DurationSeconds: run.DurationSeconds,
		URL:             run.URL,
	}
}

// waitForRun polls until the run finishes or the deadline passes. Reaching
// the deadline is not an error: the run started and keeps going, and the
// receipt says so. A failed poll is Partial so the receipt still reaches the
// caller.
func waitForRun(ctx context.Context, client *bitbucket.Client, repo ops.Repository, receipt *RunReceipt, deadline time.Time) (*RunReceipt, error) {
	delay := pollInitial
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return receipt, nil
		}
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return receipt, ops.Errorf(ops.Partial, "pipeline #%d started, but waiting stopped: %w", receipt.BuildNumber, ctx.Err())
		case <-timer.C:
		}
		delay = min(delay*2, pollMax)
		current, err := client.GetPipeline(ctx, repo.Workspace, repo.Repo, receipt.UUID)
		if err != nil {
			return receipt, ops.Errorf(ops.Partial, "pipeline #%d started, but checking on it failed: %w", receipt.BuildNumber, err)
		}
		waited := receipt.waited
		*receipt = *newRunReceipt(repo, current)
		receipt.waited = waited
		if receipt.Finished {
			return receipt, nil
		}
	}
}

func (r *RunReceipt) Markdown() (string, error) {
	var b strings.Builder
	what := fmt.Sprintf("Pipeline #%d", r.BuildNumber)
	if r.Selector != "" {
		what += " (" + r.Selector + ")"
	}
	switch {
	case r.Finished:
		fmt.Fprintf(&b, "%s finished: %s in %s.", what, r.Status, formatDuration(r.DurationSeconds, true))
	case r.waited > 0:
		fmt.Fprintf(&b, "%s is still %s after waiting %s; it keeps running. Follow it with pipeline_view %d.", what, r.Status, r.waited, r.BuildNumber)
	default:
		fmt.Fprintf(&b, "Started %s: %s. Follow it with pipeline_view %d.", what, r.Status, r.BuildNumber)
	}
	fmt.Fprintf(&b, "\n%s\n", r.URL)
	return b.String(), nil
}
