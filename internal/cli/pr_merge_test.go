package cli

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr/mergetest"
)

func TestPRMergeFlagsAndPendingExit(t *testing.T) {
	setupCLIEnv(t)
	fixture := mergetest.New()
	out, err := runAtlas(t, fixture, "--no-cache", "pr", "merge", "7", "-R", "acme/widgets", "--strategy", "squash", "--close-source-branch=false", "--message", "Ship it", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"merge_commit": "`+mergetest.MergeCommit+`"`) {
		t.Fatalf("receipt:\n%s", out)
	}
	var body string
	for _, req := range fixture.Requests() {
		if req.Method == "POST" {
			body = req.Body
		}
	}
	if !strings.Contains(body, `"merge_strategy":"squash"`) || !strings.Contains(body, `"close_source_branch":false`) || !strings.Contains(body, `"message":"Ship it"`) {
		t.Fatalf("merge body = %s", body)
	}

	stuck := mergetest.New()
	stuck.Merge = mergetest.MergeStuck
	out, err = runAtlas(t, stuck, "--no-cache", "pr", "merge", "7", "-R", "acme/widgets", "--wait", "1s")
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("err = %v, want the pending merge to fail the command", err)
	}
	if !strings.Contains(out, "Do not merge again") {
		t.Fatalf("pending receipt not printed:\n%s", out)
	}
}

func TestPRMergeRejectsUnknownStrategy(t *testing.T) {
	setupCLIEnv(t)
	fixture := mergetest.New()
	_, err := runAtlas(t, fixture, "--no-cache", "pr", "merge", "7", "-R", "acme/widgets", "--strategy", "octopus")
	if err == nil || !strings.Contains(err.Error(), "expected one of merge_commit") {
		t.Fatalf("err = %v", err)
	}
	if fixture.Posted() {
		t.Fatal("posted a merge with an invalid strategy")
	}
}
