package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr/mergetest"
)

func TestPRChecksExitsNonZeroOnFailedChecks(t *testing.T) {
	setupCLIEnv(t)
	fixture := mergetest.New()
	fixture.Statuses[mergetest.Head] = "[" + mergetest.Status("build", "Build", "FAILED") + "," + mergetest.Status("lint", "Lint", "SUCCESSFUL") + "]"
	out, err := runAtlas(t, fixture, "--no-cache", "pr", "checks", "7", "-R", "acme/widgets")
	if err == nil || !strings.Contains(err.Error(), "checks failed: Build (FAILED)") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "| FAILED | Build | build |") {
		t.Fatalf("output written before failing:\n%s", out)
	}
}

func TestPRChecksNoChecksIsNotAFailure(t *testing.T) {
	setupCLIEnv(t)
	out, err := runAtlas(t, mergetest.New(), "--no-cache", "pr", "checks", "7", "-R", "acme/widgets", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Overall string `json:"overall"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if result.Overall != "none" || result.Commit != mergetest.Head {
		t.Fatalf("result = %+v", result)
	}
}

func TestPRChecksCommitNeedsNoGit(t *testing.T) {
	setupCLIEnv(t)
	t.Chdir(t.TempDir())
	fixture := mergetest.New()
	fixture.Statuses["0011aabb"] = "[" + mergetest.Status("build", "Build", "INPROGRESS") + "]"
	out, err := runAtlas(t, fixture, "--no-cache", "pr", "checks", "--commit", "0011aabb", "-R", "acme/widgets")
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("err = %v, want a pending failure", err)
	}
	if !strings.HasPrefix(out, "# Checks for commit 0011aabb (acme/widgets)") {
		t.Fatalf("output:\n%s", out)
	}
}
