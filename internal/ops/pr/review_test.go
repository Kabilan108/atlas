package pr_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

const reviewComments = `[{"body":"inline","path":"src/login.go","line":12},{"body":"old","path":"src/login.go","line":8,"side":"old"},{"body":"reply","reply_to":101}]`

func TestReview(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.Review, env, `{"repo":"acme/widgets","pr":"42","action":"approve","body":"summary","comments":`+reviewComments+`}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"pull_request":42,"action":"approve","done":true,"state":"approved","summary_comment":204,"results":[{"index":0,"status":"ok","id":201},{"index":1,"status":"ok","id":202},{"index":2,"status":"ok","id":203}]}`
	if result.Text != want {
		t.Fatalf("receipt = %s\nwant      %s", result.Text, want)
	}
	wantCalls(t, fixture,
		"GET "+prPath,
		"POST "+prPath+"/comments", `  {"content":{"raw":"inline"},"inline":{"path":"src/login.go","to":12}}`,
		"POST "+prPath+"/comments", `  {"content":{"raw":"old"},"inline":{"path":"src/login.go","from":8}}`,
		"POST "+prPath+"/comments", `  {"content":{"raw":"reply"},"parent":{"id":101}}`,
		"POST "+prPath+"/comments", `  {"content":{"raw":"summary"}}`,
		"POST "+prPath+"/approve",
	)
	if out := result.Value.(*pr.ReviewResult); out.Results != nil || len(out.Comments) != 4 {
		t.Fatalf("a complete review's JSON has results %v and %d comments", out.Results, len(out.Comments))
	}
}

func TestReviewChecksEveryCommentFirst(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	_, err := invoke(t, pr.Review, env, `{"repo":"acme/widgets","pr":"42","action":"approve","comments":[{"body":"ok"},{"body":"bad","path":"a.go","side":"old"}]}`)
	wantInvalid(t, err, fixture, "comments[1]: side requires line")
}

func TestReviewRejects(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"repo":"acme/widgets","pr":"42"}`, "action is required"},
		{`{"repo":"acme/widgets","pr":"42","action":"comment"}`, "needs body or comments"},
		{`{"repo":"acme/widgets","pr":"42","action":"merge"}`, "action"},
	} {
		env, fixture := newTestEnv(t, "")
		_, err := invoke(t, pr.Review, env, tc.args)
		wantInvalid(t, err, fixture, tc.want)
	}
}

func TestReviewPartialWhenACommentFails(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42/comments", 1)
	result, err := invoke(t, pr.Review, env, `{"repo":"acme/widgets","pr":"42","action":"approve","body":"summary","comments":`+reviewComments+`}`)
	if ops.KindOf(err) != ops.Partial || !strings.Contains(err.Error(), "1 of 3 comments failed") || !strings.Contains(err.Error(), "failed to post comment 2") {
		t.Fatalf("err = %v, want partial", err)
	}
	want := `{"pull_request":42,"action":"approve","done":false,"results":[{"index":0,"status":"ok","id":201},{"index":1,"status":"failed","error":"server: {\"type\":\"error\",\"error\":{\"message\":\"injected failure\"}}\nHint: This is a Bitbucket server error. Try again later"},{"index":2,"status":"skipped"}]}`
	if result.Text != want {
		t.Fatalf("receipt = %s\nwant      %s", result.Text, want)
	}
	for _, call := range calls(fixture) {
		if strings.HasSuffix(call, "/approve") {
			t.Fatal("approved after a comment failed")
		}
	}
}

func TestReviewActionFailure(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42/request-changes", 0)
	result, err := invoke(t, pr.Review, env, `{"repo":"acme/widgets","pr":"42","action":"request-changes","comments":[{"body":"x"}]}`)
	if ops.KindOf(err) != ops.Partial || !strings.Contains(err.Error(), "posted 1 comments but failed to request changes on pull request") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(result.Text, `"done":false`) {
		t.Fatalf("receipt = %s", result.Text)
	}

	env, fixture = newTestEnv(t, "")
	fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42/approve", 0)
	if _, err := invoke(t, pr.Review, env, `{"repo":"acme/widgets","pr":"42","action":"approve"}`); ops.KindOf(err) != ops.Upstream {
		t.Fatalf("nothing posted: err = %v (%v), want a plain upstream error", err, ops.KindOf(err))
	}
}
