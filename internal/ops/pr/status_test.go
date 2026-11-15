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
		"## Branch feature/login\n- #42 Fix login: handle 'empty' users (feature/login → main): Grace Hopper approved, Linus Torvalds requested changes, Ken Thompson pending; 5 comments, 2 tasks\n\n" +
		"## Created by you\n- #43 [draft] Tidy the docs (docs/tidy → main): Ada Lovelace pending\n\n" +
		"## Requesting your review\n- #42 Fix login: handle 'empty' users (feature/login → main): Grace Hopper approved, Linus Torvalds requested changes, Ken Thompson pending; 5 comments, 2 tasks\n"
	if result.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", result.Text, want)
	}
	wantCalls(t, fixture, "GET /2.0/user", "GET /2.0/repositories/acme/widgets/pullrequests?fields=%2Bvalues.reviewers%2C%2Bvalues.participants&pagelen=50&sort=-updated_on&state=OPEN")

	result, err = invoke(t, pr.Status, env, `{"repo":"acme/widgets"}`)
	if err != nil {
		t.Fatal(err)
	}
	if status := result.Value.(*pr.StatusResult); status.CurrentBranch != nil || len(result.Text) >= len(want) {
		t.Fatalf("without branch: %q", result.Text)
	}
}
