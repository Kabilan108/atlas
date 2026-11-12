package pr_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/mergetest"
)

func TestMergeReturnsTheMergeCommit(t *testing.T) {
	env, fixture := mergeEnv(t)
	closeBranch := false
	receipt, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{
		Ref: prRef("7"), Strategy: "squash", Message: "Add login (#7)", CloseSourceBranch: &closeBranch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "MERGED" || receipt.MergeCommit != mergetest.MergeCommit || receipt.Destination != "main" || receipt.URL != mergetest.URL {
		t.Fatalf("receipt = %+v", receipt)
	}
	var body map[string]any
	for _, req := range fixture.Requests() {
		if req.Method == "POST" {
			if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if body["merge_strategy"] != "squash" || body["message"] != "Add login (#7)" || body["close_source_branch"] != false {
		t.Fatalf("merge body = %v", body)
	}
	text, _ := receipt.Markdown()
	if want := "Merged PR #7 into main (acme/widgets) as " + mergetest.MergeCommit + ".\n" + mergetest.URL + "\n"; text != want {
		t.Fatalf("markdown = %q, want %q", text, want)
	}
}

func TestMergeOmitsUnsetOptions(t *testing.T) {
	env, fixture := mergeEnv(t)
	if _, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{Ref: prRef("7")}); err != nil {
		t.Fatal(err)
	}
	for _, req := range fixture.Requests() {
		if req.Method == "POST" && (strings.Contains(req.Body, "merge_strategy") || strings.Contains(req.Body, "close_source_branch")) {
			t.Fatalf("body sets options the caller left unset: %s", req.Body)
		}
	}
}

func TestMergePollsAQueuedMergeToSuccess(t *testing.T) {
	env, fixture := mergeEnv(t)
	fixture.Merge, fixture.TaskPolls = mergetest.MergeQueued, 2
	receipt, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{Ref: prRef("7")})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "MERGED" || receipt.MergeCommit != mergetest.MergeCommit || receipt.TaskURL != "" {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func TestMergeDeadlineReturnsAPendingReceipt(t *testing.T) {
	env, fixture := mergeEnv(t)
	fixture.Merge = mergetest.MergeStuck
	start := time.Now()
	receipt, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{Ref: prRef("7"), WaitSeconds: 1})
	if err != nil {
		t.Fatalf("an accepted merge must not come back as an error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s past a 1s deadline", elapsed)
	}
	if receipt.State != pr.MergePending || !strings.HasSuffix(receipt.TaskURL, "/pullrequests/7/merge/task-status/t1") {
		t.Fatalf("receipt = %+v", receipt)
	}
	text, _ := receipt.Markdown()
	if !strings.Contains(text, "Do not merge again") || !strings.Contains(text, receipt.TaskURL) {
		t.Fatalf("markdown = %s", text)
	}
}

func TestMergeRefusalsPassBitbucketsReasonThrough(t *testing.T) {
	tests := []struct {
		mode    mergetest.MergeMode
		kind    ops.Kind
		message string
	}{
		{mergetest.MergeRefused, ops.Invalid, mergetest.RefusalMessage},
		{mergetest.MergeConflict, ops.Conflict, "Merge conflicts in src/login.go"},
	}
	for _, tc := range tests {
		env, fixture := mergeEnv(t)
		fixture.Merge = tc.mode
		_, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{Ref: prRef("7")})
		if ops.KindOf(err) != tc.kind {
			t.Fatalf("err = %v (kind %v), want %v", err, ops.KindOf(err), tc.kind)
		}
		for _, want := range []string{"refused to merge PR #7", tc.message, "pr_checks"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
		}
	}
}

func TestMergeRefusesAPRThatIsNotOpen(t *testing.T) {
	env, fixture := mergeEnv(t)
	fixture.State = "MERGED"
	_, err := pr.Merge.Call(context.Background(), env, pr.MergeInput{Ref: prRef("7")})
	if ops.KindOf(err) != ops.Conflict || !strings.Contains(err.Error(), "PR #7 is MERGED") {
		t.Fatalf("err = %v", err)
	}
	if fixture.Posted() {
		t.Fatal("posted a merge for a merged PR")
	}
}

func TestMergeValidatesBeforeCalling(t *testing.T) {
	env, fixture := mergeEnv(t)
	for _, in := range []pr.MergeInput{
		{Ref: prRef("7"), Strategy: "octopus"},
		{Ref: prRef("7"), WaitSeconds: -1},
		{Ref: prRef("7"), WaitSeconds: 601},
	} {
		if _, err := pr.Merge.Call(context.Background(), env, in); ops.KindOf(err) != ops.Invalid {
			t.Errorf("%+v: err = %v, want invalid", in, err)
		}
	}
	if len(fixture.Requests()) != 0 {
		t.Fatal("invalid input reached Bitbucket")
	}
}
