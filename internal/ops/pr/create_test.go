package pr_test

import (
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestCreate(t *testing.T) {
	env, fixture := newTestEnv(t, "acme")
	result, err := invoke(t, pr.Create, env, `{"repo":"widgets","title":"Add login","head":"feature/login","body":"Checklist:\n- tests","reviewers":["@557058:linus"],"draft":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":44,"title":"Add login","state":"OPEN","draft":true,"source":"feature/login","destination":"main","url":"https://bitbucket.org/acme/widgets/pull-requests/44"}`; result.Text != want {
		t.Fatalf("receipt = %s\nwant      %s", result.Text, want)
	}
	wantCalls(t, fixture,
		"GET /2.0/repositories/acme/widgets",
		"GET /2.0/users/557058:linus",
		"POST /2.0/repositories/acme/widgets/pullrequests",
		`  {"draft":true,"title":"Add login","description":"Checklist:\n\n- tests","source":{"branch":{"name":"feature/login"}},"destination":{"branch":{"name":"main"}},"reviewers":[{"uuid":"{linus-uuid}","username":"","nickname":"linus","display_name":"Linus Torvalds","account_id":"557058:linus","links":{"self":{"href":""},"html":{"href":""},"avatar":{"href":""}}}]}`,
	)
	if pr.Create.Destructive || pr.Create.ReadOnly {
		t.Fatal("pr_create is neither read-only nor destructive")
	}
}

func TestCreateWithBaseSkipsRepoLookup(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	if _, err := invoke(t, pr.Create, env, `{"repo":"acme/widgets","title":"Backport","head":"feature/login","base":"release"}`); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, fixture, "POST /2.0/repositories/acme/widgets/pullrequests",
		`  {"title":"Backport","source":{"branch":{"name":"feature/login"}},"destination":{"branch":{"name":"release"}}}`)
}

func TestCreateRejects(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"repo":"acme/widgets","head":"x"}`, "title is required"},
		{`{"repo":"acme/widgets","title":"x"}`, "head is required"},
		{`{"title":"x","head":"y"}`, "repo is required"},
	} {
		env, fixture := newTestEnv(t, "")
		_, err := invoke(t, pr.Create, env, tc.args)
		wantInvalid(t, err, fixture, tc.want)
	}
	env, fixture := newTestEnv(t, "")
	if _, err := invoke(t, pr.Create, env, `{"repo":"acme/widgets","title":"x","head":"y","base":"main","reviewers":["nobody"]}`); err == nil {
		t.Fatal("unknown reviewer accepted")
	}
	wantCalls(t, fixture, "GET /2.0/users/nobody")
}
