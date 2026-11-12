package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

const (
	defaultTailBytes = 12000
	defaultMaxBytes  = 48000
	// Unlimited as tail_bytes or max_bytes lifts that cap. The CLI uses it
	// for --full.
	Unlimited = -1
)

// LogInput is the input to pipeline_log.
type LogInput struct {
	ops.RepoRef
	Builds    []string `json:"builds" jsonschema:"build numbers (2090 or #2090) or pipeline UUIDs, at most 10"`
	Steps     []string `json:"steps,omitempty" jsonschema:"step names (any case) or UUIDs; default: failed steps, else running ones"`
	TailBytes int      `json:"tail_bytes,omitempty" jsonschema:"bytes from the end of each step log, default 12000"`
	MaxBytes  int      `json:"max_bytes,omitempty" jsonschema:"cap across all logs in the call, default 48000"`
}

// LogResult holds one entry per requested build, in order.
type LogResult struct {
	Repo    string      `json:"repo"`
	Results []BuildLogs `json:"results"`
}

// BuildLogs is the logs of the chosen steps of one build.
type BuildLogs struct {
	Index       int       `json:"index"`
	Build       string    `json:"build"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	BuildNumber int       `json:"build_number,omitempty"`
	RunStatus   string    `json:"run_status,omitempty"`
	Note        string    `json:"note,omitempty"`
	Steps       []StepLog `json:"steps,omitempty"`
}

// StepLog is the tail of one step's log. Skipped says why no log is shown.
type StepLog struct {
	Name       string `json:"name"`
	UUID       string `json:"uuid"`
	Status     string `json:"status"`
	Log        string `json:"log"`
	Bytes      int    `json:"bytes"`
	TotalBytes int64  `json:"total_bytes"`
	Truncated  bool   `json:"truncated,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
}

var Log = &ops.Op[LogInput, *LogResult]{
	Meta: ops.Meta{
		Name:        "pipeline_log",
		Description: "Tail the step logs of one or more Bitbucket Pipelines runs: failed steps by default, or the steps you name. Each log is cut to its last tail_bytes, all logs to max_bytes.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      readLogs,
	Markdown: (*LogResult).Markdown,
}

func readLogs(ctx context.Context, env ops.Env, in LogInput) (*LogResult, error) {
	builds, err := parseBuilds(in.Builds)
	if err != nil {
		return nil, err
	}
	tail, err := byteCap("tail_bytes", in.TailBytes, defaultTailBytes)
	if err != nil {
		return nil, err
	}
	budget, err := byteCap("max_bytes", in.MaxBytes, defaultMaxBytes)
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
	reader := &logReader{client: client, repo: repo, tail: tail, budget: budget}
	result := &LogResult{Repo: repo.String(), Results: make([]BuildLogs, len(builds))}
	err = eachBuild(builds, func(i int, build string) error {
		entry := &result.Results[i]
		entry.Index, entry.Build, entry.Status = i, build, entryOK
		if err := reader.read(ctx, entry, build, in.Steps); err != nil {
			entry.Status, entry.Error = entryFailed, errorText(err)
			return err
		}
		return nil
	})
	return result, err
}

func byteCap(name string, value, def int) (int, error) {
	switch {
	case value == 0:
		return def, nil
	case value == Unlimited:
		return Unlimited, nil
	case value < 0:
		return 0, ops.Errorf(ops.Invalid, "%s must be positive", name)
	}
	return value, nil
}

type logReader struct {
	client *bitbucket.Client
	repo   ops.Repository
	tail   int
	// budget is the bytes left for the rest of the call, or Unlimited.
	budget int
}

func (r *logReader) read(ctx context.Context, entry *BuildLogs, build string, names []string) error {
	pipeline, err := r.client.GetPipeline(ctx, r.repo.Workspace, r.repo.Repo, build)
	if err != nil {
		return err
	}
	entry.BuildNumber, entry.RunStatus = pipeline.BuildNumber, pipeline.State.Status()
	steps, err := r.client.ListPipelineSteps(ctx, r.repo.Workspace, r.repo.Repo, pipeline.UUID)
	if err != nil {
		return fmt.Errorf("steps: %w", err)
	}
	chosen, err := chooseSteps(steps, names)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		entry.Note = fmt.Sprintf("No failed or running steps. Name steps to read their logs: %s.", stepNames(steps))
		return nil
	}
	var failed error
	for _, step := range chosen {
		log, err := r.stepLog(ctx, pipeline.UUID, step)
		if err != nil && failed == nil {
			failed = fmt.Errorf("step %q log: %w", step.Name, err)
		}
		entry.Steps = append(entry.Steps, log)
	}
	return failed
}

func (r *logReader) stepLog(ctx context.Context, pipelineUUID string, step bitbucket.PipelineStep) (StepLog, error) {
	out := StepLog{Name: step.Name, UUID: step.UUID, Status: step.State.Status(), TotalBytes: -1}
	want := r.tail
	if r.budget != Unlimited && (want == Unlimited || want > r.budget) {
		want = r.budget
	}
	if want == 0 {
		out.Skipped = "the call's max_bytes is used up; ask for fewer builds or steps, or raise max_bytes"
		return out, nil
	}
	byteRange := ""
	if want != Unlimited {
		byteRange = fmt.Sprintf("bytes=-%d", want)
	}
	log, err := r.client.GetPipelineStepLog(ctx, r.repo.Workspace, r.repo.Repo, pipelineUUID, step.UUID, byteRange)
	if errors.Is(err, bitbucket.ErrNotFound) {
		out.Skipped = "no log yet: the step has not run"
		return out, nil
	}
	if err != nil {
		out.Skipped = "log unavailable"
		return out, err
	}
	content := log.Content
	if want != Unlimited && len(content) > want {
		content = content[len(content)-want:]
	}
	if r.budget != Unlimited {
		r.budget -= len(content)
	}
	out.TotalBytes = log.TotalSize
	out.Truncated = log.TotalSize > int64(len(content)) || len(content) < len(log.Content)
	if out.Truncated {
		if newline := bytes.IndexByte(content, '\n'); newline >= 0 && newline < len(content)-1 {
			content = content[newline+1:]
		}
	}
	out.Log = cleanLog(content)
	out.Bytes = len(out.Log)
	return out, nil
}

// chooseSteps returns the named steps, or by default the failed steps, or
// failing those the running ones.
func chooseSteps(steps []bitbucket.PipelineStep, names []string) ([]bitbucket.PipelineStep, error) {
	if len(names) == 0 {
		var failed, running []bitbucket.PipelineStep
		for _, step := range steps {
			switch {
			case step.State.Result != nil && (step.State.Result.Name == "FAILED" || step.State.Result.Name == "ERROR"):
				failed = append(failed, step)
			case step.State.Name == "IN_PROGRESS":
				running = append(running, step)
			}
		}
		if len(failed) > 0 {
			return failed, nil
		}
		return running, nil
	}
	var chosen []bitbucket.PipelineStep
	for _, name := range names {
		step, ok := findStep(steps, name)
		if !ok {
			return nil, ops.Errorf(ops.NotFound, "no step %q; steps: %s", name, stepNames(steps))
		}
		chosen = append(chosen, step)
	}
	return chosen, nil
}

func findStep(steps []bitbucket.PipelineStep, name string) (bitbucket.PipelineStep, bool) {
	name = strings.TrimSpace(name)
	bare := strings.Trim(name, "{}")
	for _, step := range steps {
		if strings.EqualFold(step.Name, name) || strings.EqualFold(strings.Trim(step.UUID, "{}"), bare) {
			return step, true
		}
	}
	return bitbucket.PipelineStep{}, false
}

func stepNames(steps []bitbucket.PipelineStep) string {
	if len(steps) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, fmt.Sprintf("%q (%s)", step.Name, step.State.Status()))
	}
	return strings.Join(names, ", ")
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func cleanLog(content []byte) string {
	text := ansiEscape.ReplaceAllString(string(content), "")
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func (r *LogResult) Markdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pipeline logs in %s\n", r.Repo)
	for _, entry := range r.Results {
		b.WriteString("\n")
		if entry.BuildNumber == 0 {
			fmt.Fprintf(&b, "## %s\n\nFailed: %s\n", entry.Build, entry.Error)
			continue
		}
		fmt.Fprintf(&b, "## #%d %s\n", entry.BuildNumber, entry.RunStatus)
		if entry.Error != "" {
			fmt.Fprintf(&b, "\nFailed: %s\n", entry.Error)
		}
		if entry.Note != "" {
			fmt.Fprintf(&b, "\n%s\n", entry.Note)
		}
		for _, step := range entry.Steps {
			fmt.Fprintf(&b, "\n### Step %q: %s", step.Name, step.Status)
			if step.Skipped != "" {
				fmt.Fprintf(&b, "\n\n(%s)\n", step.Skipped)
				continue
			}
			if step.Truncated {
				if step.TotalBytes >= 0 {
					fmt.Fprintf(&b, " (last %d of %d bytes)", step.Bytes, step.TotalBytes)
				} else {
					fmt.Fprintf(&b, " (last %d bytes)", step.Bytes)
				}
			}
			fence := codeFence(step.Log)
			fmt.Fprintf(&b, "\n\n%stext\n%s", fence, step.Log)
			if !strings.HasSuffix(step.Log, "\n") {
				b.WriteString("\n")
			}
			b.WriteString(fence + "\n")
		}
	}
	return b.String(), nil
}

// codeFence is a backtick fence longer than any backtick run in text.
func codeFence(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}
