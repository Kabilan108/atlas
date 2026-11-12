package pr_test

import (
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestStatus(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.Status, env, `{"repo":"acme/widgets","branch":"feature/login"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "Open pull requests in acme/widgets for Grace Hopper\n\n" +
		"## Branch feature/login\n- #42 Fix login: handle 'empty' users (feature/login → main): 1 approved, 1 changes requested, 1 pending; 5 comments, 2 tasks\n\n" +
		"## Created by you\n- #43 [draft] Tidy the docs (docs/tidy → main): 1 pending\n\n" +
		"## Requesting your review\n- #42 Fix login: handle 'empty' users (feature/login → main): 1 approved, 1 changes requested, 1 pending; 5 comments, 2 tasks\n"
	if result.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", result.Text, want)
	}
	wantCalls(t, fixture, "GET /2.0/user", "GET /2.0/repositories/acme/widgets/pullrequests?state=OPEN")

	result, err = invoke(t, pr.Status, env, `{"repo":"acme/widgets"}`)
	if err != nil {
		t.Fatal(err)
	}
	if status := result.Value.(*pr.StatusResult); status.CurrentBranch != nil || len(result.Text) >= len(want) {
		t.Fatalf("without branch: %q", result.Text)
	}
}
