package pr_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestClose(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.Close, env, `{"repo":"acme/widgets","pr":"42","body":"Superseded"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, `"state":"DECLINED"`) {
		t.Fatalf("receipt = %s", result.Text)
	}
	wantCalls(t, fixture, "GET "+prPath, "POST "+prPath+"/comments", `  {"content":{"raw":"Superseded"}}`, "POST "+prPath+"/decline")
	if !pr.Close.Destructive {
		t.Fatal("pr_close must be destructive")
	}
}

func TestCloseSaysTheCommentPosted(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42/decline", 0)
	_, err := invoke(t, pr.Close, env, `{"repo":"acme/widgets","pr":"42","body":"bye"}`)
	if err == nil || !strings.HasPrefix(err.Error(), "posted closing comment but failed to close pull request") {
		t.Fatalf("err = %v", err)
	}
}
