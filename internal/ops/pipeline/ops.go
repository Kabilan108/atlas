// Package pipeline holds the Bitbucket Pipelines operations.
package pipeline

import (
	"fmt"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// Ops returns the pipeline ops in a stable order.
func Ops() []ops.Def {
	return []ops.Def{
		List,
		View,
		Log,
		Start,
		Stop,
	}
}

// maxBuilds bounds the builds one view or log call reads.
const maxBuilds = 10

const stateCompleted = "COMPLETED"

// Run is one pipeline run.
type Run struct {
	BuildNumber     int        `json:"build_number"`
	UUID            string     `json:"uuid"`
	Status          string     `json:"status"`
	Branch          string     `json:"branch,omitempty"`
	Commit          string     `json:"commit,omitempty"`
	Selector        string     `json:"selector,omitempty"`
	Trigger         string     `json:"trigger,omitempty"`
	Creator         string     `json:"creator,omitempty"`
	CreatedOn       time.Time  `json:"created_on"`
	CompletedOn     *time.Time `json:"completed_on,omitempty"`
	DurationSeconds int        `json:"duration_seconds"`
	URL             string     `json:"url"`
}

func newRun(repo ops.Repository, p *bitbucket.Pipeline) Run {
	return Run{
		BuildNumber:     p.BuildNumber,
		UUID:            p.UUID,
		Status:          p.State.Status(),
		Branch:          p.Target.Branch(),
		Commit:          p.Target.CommitHash(),
		Selector:        selector(p.Target.Selector),
		Trigger:         trigger(p.Trigger),
		Creator:         p.Creator.DisplayName,
		CreatedOn:       p.CreatedOn,
		CompletedOn:     p.CompletedOn,
		DurationSeconds: p.DurationInSeconds,
		URL:             bitbucket.PipelineWebURL(repo.Workspace, repo.Repo, p.BuildNumber),
	}
}

// Finished reports whether the run has a final result.
func (r Run) Finished() bool {
	return r.CompletedOn != nil
}

func finished(p *bitbucket.Pipeline) bool {
	return p.State.Name == stateCompleted
}

func selector(s *bitbucket.PipelineSelector) string {
	if s == nil || s.Type == "" {
		return ""
	}
	if s.Pattern == "" {
		return s.Type
	}
	return s.Type + ":" + s.Pattern
}

func trigger(t bitbucket.PipelineTrigger) string {
	if t.Name != "" {
		return t.Name
	}
	return strings.ToUpper(strings.TrimPrefix(t.Type, "pipeline_trigger_"))
}

// Step is one step of a pipeline run.
type Step struct {
	UUID            string     `json:"uuid"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	StartedOn       *time.Time `json:"started_on,omitempty"`
	CompletedOn     *time.Time `json:"completed_on,omitempty"`
	DurationSeconds int        `json:"duration_seconds"`
}

func newStep(s bitbucket.PipelineStep) Step {
	return Step{
		UUID:            s.UUID,
		Name:            s.Name,
		Status:          s.State.Status(),
		StartedOn:       s.StartedOn,
		CompletedOn:     s.CompletedOn,
		DurationSeconds: s.DurationInSeconds,
	}
}

// parseBuilds validates build references: build numbers (2090 or #2090) or
// pipeline UUIDs, with or without braces.
func parseBuilds(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ops.Errorf(ops.Invalid, "builds is required: give build numbers such as 2090 (see pipeline_list) or pipeline UUIDs")
	}
	if len(raw) > maxBuilds {
		return nil, ops.Errorf(ops.Invalid, "%d builds requested; read at most %d per call", len(raw), maxBuilds)
	}
	builds := make([]string, 0, len(raw))
	for _, value := range raw {
		build, err := parseBuild(value)
		if err != nil {
			return nil, err
		}
		builds = append(builds, build)
	}
	return builds, nil
}

func parseBuild(raw string) (string, error) {
	build := strings.TrimPrefix(strings.TrimSpace(raw), "#")
	bare := strings.Trim(build, "{}")
	valid := bare != ""
	for _, r := range bare {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == '-') {
			valid = false
			break
		}
	}
	if !valid {
		return "", ops.Errorf(ops.Invalid, "invalid build %q: expected a build number such as 2090 or a pipeline UUID", raw)
	}
	return build, nil
}

// Batch entry statuses.
const (
	entryOK     = "ok"
	entryFailed = "failed"
)

// eachBuild calls read for every build in order and counts failures. read
// records its own result, including the error on a failure.
func eachBuild(builds []string, read func(i int, build string) error) error {
	failed := 0
	for i, build := range builds {
		if err := read(i, build); err != nil {
			failed++
		}
	}
	return ops.PartialOf(failed, len(builds), "builds")
}

func errorText(err error) string {
	return ops.KindOf(err).String() + ": " + err.Error()
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func formatDuration(seconds int, done bool) string {
	if seconds <= 0 && !done {
		return "running"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func cell(s string) string {
	if s == "" {
		return "-"
	}
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}

func describeRun(b *strings.Builder, r Run) {
	fmt.Fprintf(b, "## #%d %s", r.BuildNumber, r.Status)
	if r.Selector != "" {
		fmt.Fprintf(b, " · %s", r.Selector)
	}
	if r.Branch != "" {
		fmt.Fprintf(b, " on %s", r.Branch)
	}
	if r.Commit != "" {
		fmt.Fprintf(b, " @ %s", shortHash(r.Commit))
	}
	b.WriteString("\n\n")
	if r.Trigger != "" {
		fmt.Fprintf(b, "Trigger %s", r.Trigger)
		if r.Creator != "" {
			fmt.Fprintf(b, " by %s", r.Creator)
		}
		b.WriteString(" · ")
	}
	fmt.Fprintf(b, "started %s · %s · %s\n", formatTime(&r.CreatedOn), formatDuration(r.DurationSeconds, r.Finished()), r.URL)
}
