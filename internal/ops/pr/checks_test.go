package pr_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/mergetest"
)

func mergeEnv(t *testing.T) (*opstest.Env, *mergetest.Fixture) {
	t.Helper()
	fixture := mergetest.New()
	return opstest.New("acme").WithBitbucket(t, fixture), fixture
}

func prRef(selector string) pr.Ref {
	return pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: selector}
}

func statuses(values ...string) string {
	return "[" + strings.Join(values, ",") + "]"
}

func TestChecksReadsTheHeadCommit(t *testing.T) {
	tests := []struct {
		name     string
		statuses string
		overall  string
		text     string
	}{
		{"none", "", pr.ChecksNone, "No checks reported on this commit. Nothing has run or posted a status yet, so this is not a pass."},
		{"passed", statuses(mergetest.Status("build", "Build", "SUCCESSFUL"), mergetest.Status("lint", "Lint", "SUCCESSFUL")), pr.ChecksPassed, "All 2 checks passed."},
		{"pending", statuses(mergetest.Status("build", "Build", "INPROGRESS"), mergetest.Status("lint", "Lint", "SUCCESSFUL")), pr.ChecksPending, "Pending: 1 inprogress, 1 successful."},
		{"failed", statuses(mergetest.Status("build", "Build", "FAILED"), mergetest.Status("e2e", "E2E", "STOPPED"), mergetest.Status("lint", "Lint", "INPROGRESS")), pr.ChecksFailed, "Failed: 1 failed, 1 stopped, 1 inprogress."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := mergeEnv(t)
			if tc.statuses != "" {
				fixture.Statuses[mergetest.Head] = tc.statuses
			}
			result, err := pr.Checks.Call(context.Background(), env, pr.ChecksInput{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "7"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Overall != tc.overall || result.Commit != mergetest.Head || result.PR != 7 {
				t.Fatalf("result = %+v", result)
			}
			text, err := result.Markdown()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(text, "# Checks for PR #7 at abc123def456 (acme/widgets)\n\n"+tc.text) {
				t.Fatalf("markdown:\n%s", text)
			}
			if tc.overall != pr.ChecksNone && !strings.Contains(text, " | Lint | lint | 2026-11-10T12:00:00Z | https://ci.example/lint |") {
				t.Fatalf("markdown lacks the lint row:\n%s", text)
			}
			data, _ := json.Marshal(result)
			if !strings.Contains(string(data), `"checks":[`) {
				t.Fatalf("JSON must list checks even when empty: %s", data)
			}
		})
	}
}

func TestChecksFailingListsFailedAndStopped(t *testing.T) {
	env, fixture := mergeEnv(t)
	fixture.Statuses[mergetest.Head] = statuses(mergetest.Status("build", "Build", "FAILED"), mergetest.Status("e2e", "E2E", "STOPPED"), mergetest.Status("lint", "Lint", "SUCCESSFUL"))
	result, err := pr.Checks.Call(context.Background(), env, pr.ChecksInput{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "7"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, check := range result.Failing() {
		names = append(names, check.Key)
	}
	if strings.Join(names, ",") != "build,e2e" {
		t.Fatalf("failing = %v", names)
	}
}

func TestChecksForACommitNeedsNoPR(t *testing.T) {
	env, fixture := mergeEnv(t)
	fixture.Statuses["0011aabb"] = statuses(mergetest.Status("build", "Build", "SUCCESSFUL"))
	result, err := pr.Checks.Call(context.Background(), env, pr.ChecksInput{RepoRef: ops.RepoRef{Repo: "widgets"}, Commit: "0011aabb"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Overall != pr.ChecksPassed || result.PR != 0 {
		t.Fatalf("result = %+v", result)
	}
	requests := fixture.Requests()
	if len(requests) != 1 || requests[0].Path != "/2.0/repositories/acme/widgets/commit/0011aabb/statuses" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestChecksWarnsWhenTheCommitIsNotTheHead(t *testing.T) {
	env, _ := mergeEnv(t)
	result, err := pr.Checks.Call(context.Background(), env, pr.ChecksInput{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "7", Commit: "99887766"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := result.Markdown()
	if !result.HeadMismatch() || !strings.Contains(text, "Warning: PR #7's head is now abc123def456, not 99887766.") {
		t.Fatalf("markdown:\n%s", text)
	}

	result, err = pr.Checks.Call(context.Background(), env, pr.ChecksInput{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "7", Commit: mergetest.Head + "0011223344556677889900aa"})
	if err != nil {
		t.Fatal(err)
	}
	if result.HeadMismatch() {
		t.Fatal("a full SHA extending the short head is not a mismatch")
	}
}

func TestChecksNeedsAPROrCommit(t *testing.T) {
	env, fixture := mergeEnv(t)
	for _, in := range []pr.ChecksInput{
		{RepoRef: ops.RepoRef{Repo: "acme/widgets"}},
		{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, Commit: "abc/../x"},
	} {
		if _, err := pr.Checks.Call(context.Background(), env, in); ops.KindOf(err) != ops.Invalid {
			t.Errorf("%+v: err = %v, want invalid", in, err)
		}
	}
	if len(fixture.Requests()) != 0 {
		t.Fatal("invalid input reached Bitbucket")
	}
}
