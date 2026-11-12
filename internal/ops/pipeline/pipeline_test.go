package pipeline_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/pipeline"
	"github.com/kabilan108/atlas/internal/ops/pipeline/pipelinetest"
)

const repo = "acme/widgets"

func newEnv(t *testing.T) (*opstest.Env, *pipelinetest.Fixture) {
	t.Helper()
	fixture := pipelinetest.New()
	return opstest.New("acme").WithBitbucket(t, fixture), fixture
}

func markdown(t *testing.T, render func() (string, error)) string {
	t.Helper()
	text, err := render()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q\n--- output ---\n%s", want, text)
		}
	}
}

func TestListFiltersAndShowsRunDetails(t *testing.T) {
	env, fixture := newEnv(t)
	result, err := pipeline.List.Call(context.Background(), env, pipeline.ListInput{
		RepoRef: ops.RepoRef{Repo: "widgets"}, Branch: "testing", Pattern: "db-backup", Commit: "333ec33226f2aa11bb22cc33dd44ee55ff667788",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.Requests()[0].Path
	for _, want := range []string{"target.branch=testing", "target.selector.pattern=db-backup", "target.commit.hash=333ec33226f2", "pagelen=20", "sort=-created_on"} {
		if !strings.Contains(path, want) {
			t.Errorf("list path %s lacks %s", path, want)
		}
	}
	if len(result.Pipelines) != 2 {
		t.Fatalf("got %d runs, want 2095 and 2090", len(result.Pipelines))
	}
	mustContain(t, markdown(t, result.Markdown),
		"# Pipelines in acme/widgets (branch testing, commit 333ec33226f2aa11bb22cc33dd44ee55ff667788, pattern db-backup)",
		"| 2090 | FAILED | testing | 333ec33226f2 | custom:db-backup | MANUAL | Merge Console | 2026-11-02T18:45:37Z | 1m24s |",
		"| 2095 | SUCCESSFUL |")

	empty, err := pipeline.List.Call(context.Background(), env, pipeline.ListInput{RepoRef: ops.RepoRef{Repo: repo}, Pattern: "nightly"})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, markdown(t, empty.Markdown), "No pipeline runs match.")
}

func TestListRejectsBadLimitsBeforeCalling(t *testing.T) {
	env, fixture := newEnv(t)
	for _, limit := range []int{-1, 101} {
		_, err := pipeline.List.Call(context.Background(), env, pipeline.ListInput{RepoRef: ops.RepoRef{Repo: repo}, Limit: limit})
		if ops.KindOf(err) != ops.Invalid {
			t.Errorf("limit %d: err = %v, want invalid", limit, err)
		}
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestViewSeveralBuildsReportsEachEntry(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.View.Call(context.Background(), env, pipeline.ViewInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090", "#2096", "99999"},
	})
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want partial", err)
	}
	statuses := []string{}
	for i, entry := range result.Results {
		if entry.Index != i {
			t.Errorf("entry %d has index %d", i, entry.Index)
		}
		statuses = append(statuses, entry.Status)
	}
	if got := strings.Join(statuses, ","); got != "ok,ok,failed" {
		t.Fatalf("statuses = %s", got)
	}
	if !strings.HasPrefix(result.Results[2].Error, "not_found:") {
		t.Errorf("missing build error = %q", result.Results[2].Error)
	}
	mustContain(t, markdown(t, result.Markdown),
		"## #2090 FAILED · custom:db-backup on testing @ 333ec33226f2",
		"Trigger MANUAL by Merge Console · started 2026-11-02T18:45:37Z · 1m24s · https://bitbucket.org/acme/widgets/pipelines/results/2090",
		"| backup | FAILED | 2026-11-02T18:45:38Z | 1m12s |",
		"| deploy | NOT_RUN | - | - |",
		"## #2096 RUNNING · branches:main on main",
		"| backup | RUNNING | 2026-11-02T18:45:38Z | running |",
		"## 99999\n\nFailed: not_found:")
}

func TestViewSingleMissingBuildIsStillPartial(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.View.Call(context.Background(), env, pipeline.ViewInput{RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"99999"}})
	if ops.KindOf(err) != ops.Partial || result == nil || result.Results[0].Status != "failed" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestViewValidatesBuilds(t *testing.T) {
	env, fixture := newEnv(t)
	for _, builds := range [][]string{nil, {"../x"}, {"2090?x"}, make([]string, 11)} {
		_, err := pipeline.View.Call(context.Background(), env, pipeline.ViewInput{RepoRef: ops.RepoRef{Repo: repo}, Builds: builds})
		if ops.KindOf(err) != ops.Invalid {
			t.Errorf("builds %q: err = %v, want invalid", builds, err)
		}
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestViewAndLogTakeResultURLs(t *testing.T) {
	env, _ := newEnv(t)
	view, err := pipeline.View.Call(context.Background(), env, pipeline.ViewInput{Builds: []string{
		"https://bitbucket.org/acme/widgets/pipelines/results/2090",
		"https://bitbucket.org/acme/widgets/addon/pipelines/home#!/results/2096/steps/{x}",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if view.Repo != repo || view.Results[0].Build != "2090" || view.Results[1].Build != "2096" {
		t.Fatalf("view = %+v", view)
	}
	logs, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: "widgets"}, Builds: []string{"https://bitbucket.org/acme/widgets/pipelines/results/2090"},
	})
	if err != nil || logs.Results[0].BuildNumber != 2090 {
		t.Fatalf("logs = %+v, err = %v", logs, err)
	}
}

func TestBuildURLsMustShareTheRepo(t *testing.T) {
	env, fixture := newEnv(t)
	for _, in := range []pipeline.ViewInput{
		{Builds: []string{"https://bitbucket.org/acme/widgets/pipelines/results/1", "https://bitbucket.org/acme/gadgets/pipelines/results/2"}},
		{RepoRef: ops.RepoRef{Repo: "acme/gadgets"}, Builds: []string{"https://bitbucket.org/acme/widgets/pipelines/results/1"}},
		{Builds: []string{"https://bitbucket.org/acme/widgets/pipelines/results/1", "2090"}, RepoRef: ops.RepoRef{Repo: "acme/other"}},
	} {
		_, err := pipeline.View.Call(context.Background(), env, in)
		if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), "acme/") {
			t.Errorf("%+v: err = %v, want invalid naming the repos", in, err)
		}
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestLogDefaultsToFailedStepsWithBoundedTail(t *testing.T) {
	env, fixture := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090"}})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Results[0]
	if len(entry.Steps) != 1 || entry.Steps[0].Name != "backup" {
		t.Fatalf("steps = %+v, want only the failed backup step", entry.Steps)
	}
	step := entry.Steps[0]
	if !step.Truncated || step.TotalBytes != int64(len(pipelinetest.FailedLog)) || step.Bytes > 12000 {
		t.Fatalf("step = truncated %v, %d of %d bytes", step.Truncated, step.Bytes, step.TotalBytes)
	}
	if !strings.HasPrefix(step.Log, "backup line ") || !strings.HasSuffix(pipelinetest.FailedLog, step.Log) {
		t.Fatalf("log does not start on a line boundary at the end of the log: %q", step.Log[:40])
	}
	var ranges []string
	for _, req := range fixture.Requests() {
		if strings.HasSuffix(req.Path, "/log") {
			ranges = append(ranges, req.Range)
		}
	}
	if strings.Join(ranges, ",") != "bytes=-12000" {
		t.Fatalf("log requests used ranges %v", ranges)
	}
	mustContain(t, markdown(t, result.Markdown),
		"## #2090 FAILED",
		`### Step "backup": FAILED (last `+jsonInt(step.Bytes)+" of "+jsonInt(len(pipelinetest.FailedLog))+" bytes)")
}

func jsonInt(n int) string {
	data, _ := json.Marshal(n)
	return string(data)
}

func TestLogTotalCapSpansBuilds(t *testing.T) {
	env, fixture := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090", "2096"}, TailBytes: 3000, MaxBytes: 3050,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, second := result.Results[0].Steps[0], result.Results[1].Steps[0]
	if first.Bytes > 3000 || first.Bytes < 2900 {
		t.Fatalf("first step shows %d bytes", first.Bytes)
	}
	var ranges []string
	for _, req := range fixture.Requests() {
		if strings.HasSuffix(req.Path, "/log") {
			ranges = append(ranges, req.Range)
		}
	}
	if len(ranges) != 2 || ranges[0] != "bytes=-3000" || ranges[1] != "bytes=-50" {
		t.Fatalf("ranges = %v, want the second log cut to the 50 bytes left", ranges)
	}
	if second.Skipped != "" || second.Bytes > 50 {
		t.Fatalf("second step = %+v", second)
	}

	capped, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090", "2096"}, TailBytes: 2000, MaxBytes: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if skipped := capped.Results[1].Steps[0].Skipped; !strings.Contains(skipped, "max_bytes") {
		t.Fatalf("second step after the cap: skipped = %q", skipped)
	}
}

func TestLogMaximumCapsFetchWholeSmallLogs(t *testing.T) {
	env, fixture := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090"}, TailBytes: pipeline.MaxTailBytes, MaxBytes: pipeline.MaxCallBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if step := result.Results[0].Steps[0]; step.Truncated || step.Log != pipelinetest.FailedLog {
		t.Fatalf("log truncated=%v, %d bytes", step.Truncated, step.Bytes)
	}
	for _, req := range fixture.Requests() {
		if strings.HasSuffix(req.Path, "/log") && req.Range != "bytes=-2097152" {
			t.Fatalf("log read sent Range %q, want the per-step maximum", req.Range)
		}
	}
}

func TestLogRunningStepIsCleanedAndFenced(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2096"}})
	if err != nil {
		t.Fatal(err)
	}
	step := result.Results[0].Steps[0]
	if step.Log != "connecting\n```\nnot a fence\n```\nstill waiting\n" {
		t.Fatalf("log = %q", step.Log)
	}
	mustContain(t, markdown(t, result.Markdown), "````text\nconnecting\n```\nnot a fence", "still waiting\n````\n")
}

func TestLogNamedStepsAndMisses(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090"}, Steps: []string{"BUILD", "{s-deploy}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := result.Results[0].Steps
	if len(steps) != 2 || steps[0].Log != "build ok\n" || !strings.Contains(steps[1].Skipped, "has not run") {
		t.Fatalf("steps = %+v", steps)
	}

	result, err = pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090", "2095"}, Steps: []string{"lint"},
	})
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want partial", err)
	}
	for _, entry := range result.Results {
		if entry.Status != "failed" || !strings.Contains(entry.Error, `no step "lint"; steps: "`) {
			t.Fatalf("entry = %+v", entry)
		}
	}
}

func TestLogPassedBuildSaysWhichStepsToName(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2095"}})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, markdown(t, result.Markdown), `No failed or running steps. Name steps to read their logs: "backup" (SUCCESSFUL).`)
}

func TestRunStartsCustomPipelineWithVariables(t *testing.T) {
	env, fixture := newEnv(t)
	receipt, err := pipeline.Start.Call(context.Background(), env, pipeline.RunInput{
		RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", Pipeline: "db-backup",
		Variables: []pipeline.Variable{{Key: "TARGET", Value: "iam"}, {Key: "TOKEN", Value: "s3cret", Secured: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.BuildNumber != pipelinetest.Started || receipt.Finished || receipt.URL != "https://bitbucket.org/acme/widgets/pipelines/results/2100" {
		t.Fatalf("receipt = %+v", receipt)
	}
	requests := fixture.Requests()
	if len(requests) != 1 || requests[0].Method != "POST" {
		t.Fatalf("requests = %+v, want one POST and no polling", requests)
	}
	var body struct {
		Target struct {
			Type     string `json:"type"`
			RefName  string `json:"ref_name"`
			Selector struct{ Type, Pattern string }
		}
		Variables []struct {
			Key, Value string
			Secured    bool
		}
	}
	if err := json.Unmarshal([]byte(requests[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Target.RefName != "main" || body.Target.Selector.Type != "custom" || body.Target.Selector.Pattern != "db-backup" ||
		len(body.Variables) != 2 || !body.Variables[1].Secured || body.Variables[1].Value != "s3cret" {
		t.Fatalf("body = %s", requests[0].Body)
	}
	data, _ := json.Marshal(receipt)
	text := markdown(t, receipt.Markdown)
	if strings.Contains(string(data)+text, "s3cret") {
		t.Fatal("receipt echoes a secured variable")
	}
	mustContain(t, text, "Started Pipeline #2100 (custom:db-backup): PENDING. Follow it with pipeline_view 2100.")
}

func TestRunWaitsForTheResult(t *testing.T) {
	pipeline.FastPolls(t)
	env, fixture := newEnv(t)
	fixture.StartedPolls = 2
	receipt, err := pipeline.Start.Call(context.Background(), env, pipeline.RunInput{RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", WaitSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Finished || receipt.Status != "SUCCESSFUL" {
		t.Fatalf("receipt = %+v", receipt)
	}
	mustContain(t, markdown(t, receipt.Markdown), "Pipeline #2100 (custom:db-backup) finished: SUCCESSFUL in 41s.")
}

func TestRunWaitDeadlineKeepsTheReceipt(t *testing.T) {
	pipeline.FastPolls(t)
	env, _ := newEnv(t)
	receipt, err := pipeline.Start.Call(context.Background(), env, pipeline.RunInput{RepoRef: ops.RepoRef{Repo: repo}, Commit: "abc123", WaitSeconds: 1})
	if err != nil {
		t.Fatalf("a started run must not come back as an error: %v", err)
	}
	if receipt.Finished || receipt.BuildNumber != pipelinetest.Started || receipt.Status != "RUNNING" {
		t.Fatalf("receipt = %+v", receipt)
	}
	mustContain(t, markdown(t, receipt.Markdown), "is still RUNNING after waiting 1s; it keeps running. Follow it with pipeline_view 2100.")
}

func TestRunValidatesBeforeStarting(t *testing.T) {
	env, fixture := newEnv(t)
	for name, in := range map[string]pipeline.RunInput{
		"no target":  {RepoRef: ops.RepoRef{Repo: repo}},
		"long wait":  {RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", WaitSeconds: 601},
		"empty key":  {RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", Variables: []pipeline.Variable{{Value: "x"}}},
		"negative":   {RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", WaitSeconds: -1},
		"bad repo":   {RepoRef: ops.RepoRef{Repo: "acme/../x"}, Branch: "main"},
		"repo empty": {Branch: "main"},
	} {
		if _, err := pipeline.Start.Call(context.Background(), env, in); ops.KindOf(err) != ops.Invalid {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestStopRunningBuild(t *testing.T) {
	env, fixture := newEnv(t)
	receipt, err := pipeline.Stop.Call(context.Background(), env, pipeline.StopInput{RepoRef: ops.RepoRef{Repo: repo}, Build: "2096"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.BuildNumber != 2096 || receipt.WasStatus != "RUNNING" {
		t.Fatalf("receipt = %+v", receipt)
	}
	if fixture.Count("POST", "/2.0/repositories/acme/widgets/pipelines/%7Bp-2096%7D/stopPipeline") != 1 {
		t.Fatalf("requests = %+v", fixture.Requests())
	}
}

func TestStopFinishedBuildIsAConflict(t *testing.T) {
	env, fixture := newEnv(t)
	_, err := pipeline.Stop.Call(context.Background(), env, pipeline.StopInput{RepoRef: ops.RepoRef{Repo: repo}, Build: "2090"})
	if ops.KindOf(err) != ops.Conflict || !strings.Contains(err.Error(), "already finished with FAILED") {
		t.Fatalf("err = %v", err)
	}
	for _, req := range fixture.Requests() {
		if req.Method == "POST" {
			t.Fatalf("sent %s %s", req.Method, req.Path)
		}
	}
}

// MCP calls arrive as JSON and render as the same markdown the CLI prints.
func TestInvokeDecodesJSONArguments(t *testing.T) {
	env, _ := newEnv(t)
	result, err := pipeline.View.Invoke(context.Background(), env, json.RawMessage(`{"repo":"acme/widgets","builds":["2090"]}`))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, result.Text, "## #2090 FAILED", "| backup | FAILED |")

	_, err = pipeline.Start.Invoke(context.Background(), env, json.RawMessage(`{"repo":"acme/widgets","branch":"main","variables":[{"key":"A","value":"1","secure":true}]}`))
	if ops.KindOf(err) != ops.Invalid {
		t.Fatalf("misspelt variable field: err = %v, want invalid", err)
	}
}
