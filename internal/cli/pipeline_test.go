package cli

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pipeline/pipelinetest"
)

func TestPipelineListPassesFilters(t *testing.T) {
	setupCLIEnv(t)
	fixture := pipelinetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pipeline", "list", "-R", "acme/widgets", "-b", "testing", "--pattern", "db-backup", "-L", "5")
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.Requests()[0].Path
	for _, want := range []string{"target.branch=testing", "target.selector.pattern=db-backup", "pagelen=5"} {
		if !strings.Contains(path, want) {
			t.Errorf("path %s lacks %s", path, want)
		}
	}
	if !strings.Contains(out, "| 2090 | FAILED | testing |") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPipelineViewSeveralBuildsPrintsThenFails(t *testing.T) {
	setupCLIEnv(t)
	out, err := runAtlas(t, pipelinetest.New(), "--no-cache", "pipeline", "view", "-R", "acme/widgets", "2090", "99999")
	if err == nil || !strings.Contains(err.Error(), "1 of 2 builds failed") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "## #2090 FAILED") || !strings.Contains(out, "## 99999\n\nFailed: not_found") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPipelineLogStepsAndFull(t *testing.T) {
	setupCLIEnv(t)
	fixture := pipelinetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pipeline", "log", "-R", "acme/widgets", "2090", "--step", "build", "--step", "backup", "--full")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "build ok\n") || !strings.Contains(out, pipelinetest.FailedLog) {
		t.Fatalf("output lacks the whole logs:\n%.400s", out)
	}
	for _, req := range fixture.Requests() {
		if strings.HasSuffix(req.Path, "/log") && req.Range != "bytes=-2097152" {
			t.Fatalf("--full sent Range %q, want the per-step maximum", req.Range)
		}
	}
	if _, err := runAtlas(t, fixture, "--no-cache", "pipeline", "log", "-R", "acme/widgets", "2090", "--full", "--tail", "10"); err == nil {
		t.Fatal("--full with --tail must be rejected")
	}
}

func TestPipelineRunVariablesAndWaitDeadline(t *testing.T) {
	setupCLIEnv(t)
	fixture := pipelinetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pipeline", "run", "-R", "acme/widgets", "-b", "main", "-p", "db-backup",
		"--var", "TARGET=iam", "--secured-var", "TOKEN=a=b", "--wait", "1s")
	if err == nil || !strings.Contains(err.Error(), "pipeline #2100 is still PENDING") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Pipeline #2100 (custom:db-backup) is still PENDING after waiting 1s") {
		t.Fatalf("output:\n%s", out)
	}
	body := fixture.Requests()[0].Body
	if !strings.Contains(body, `{"key":"TARGET","value":"iam","secured":false}`) || !strings.Contains(body, `{"key":"TOKEN","value":"a=b","secured":true}`) {
		t.Fatalf("body = %s", body)
	}

	if _, err := runAtlas(t, pipelinetest.New(), "--no-cache", "pipeline", "run", "-R", "acme/widgets", "-b", "main", "--var", "NOEQUALS"); err == nil || !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Fatalf("err = %v", err)
	}
}

func TestPipelineRunWithoutWaitSucceedsOnStart(t *testing.T) {
	setupCLIEnv(t)
	fixture := pipelinetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pipeline", "run", "-R", "acme/widgets", "-b", "main", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"build_number": 2100`) || len(fixture.Requests()) != 1 {
		t.Fatalf("output:\n%s\nrequests: %+v", out, fixture.Requests())
	}
}

func TestPipelineStop(t *testing.T) {
	setupCLIEnv(t)
	fixture := pipelinetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pipeline", "stop", "-R", "acme/widgets", "#2096")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Asked Bitbucket to stop pipeline #2096 (was RUNNING)") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPipelineRunSecuredVarErrorsHideTheValue(t *testing.T) {
	setupCLIEnv(t)
	const secret = "synthetic-secret-77ad02"
	for _, arg := range []string{secret, "=" + secret} {
		fixture := pipelinetest.New()
		_, err := runAtlas(t, fixture, "--no-cache", "pipeline", "run", "-R", "acme/widgets", "-b", "main", "--secured-var", arg)
		if err == nil || !strings.Contains(err.Error(), "KEY=VALUE") {
			t.Fatalf("--secured-var %s: err = %v, want a KEY=VALUE error", arg, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks the secured value: %v", err)
		}
		if len(fixture.Requests()) != 0 {
			t.Fatal("a malformed variable reached Bitbucket")
		}
	}
}
