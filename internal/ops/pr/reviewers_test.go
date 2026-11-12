package pr_test

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestCreateReviewersByName(t *testing.T) {
	env, fixture := newTestEnv(t, "acme")
	result, err := invoke(t, pr.Create, env, `{"repo":"widgets","title":"x","head":"y","base":"main","reviewers":["Linus Torvalds","@grace hopper","{margaret-uuid}","557058:linus","ken"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := calls(fixture)
	if got[0] != "GET /2.0/workspaces/acme/members?pagelen=100" || got[1] != "GET /2.0/users/%7Bmargaret-uuid%7D" || got[2] != "GET /2.0/users/557058:linus" {
		t.Fatalf("requests = %v; want one member listing for every name, then /users for IDs", got)
	}
	body := got[len(got)-1]
	for _, id := range []string{"557058:linus", "557058:grace", "557058:mentioned", "557058:ken"} {
		if strings.Count(body, `"account_id":"`+id+`"`) != 1 {
			t.Errorf("payload does not name %s once: %s", id, body)
		}
	}
	if result.Value == nil {
		t.Fatal("no pull request")
	}
}

func TestReviewerNameErrors(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"ambiguous", `"reviewers":["sam lee"]`, `reviewer "sam lee" matches 2 workspace members: Sam Lee (557058:sam-a), Sam  lee (557058:sam-b); give an account ID`},
		{"missing with suggestions", `"reviewers":["Hopper"]`, `no workspace member of acme is named "Hopper"; did you mean Grace Hopper (557058:grace)`},
		{"missing", `"reviewers":["Nobody Here"]`, `no workspace member of acme is named "Nobody Here"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "acme")
			_, err := invoke(t, pr.Create, env, `{"repo":"widgets","title":"x","head":"y","base":"main",`+tc.args+`}`)
			if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v (%v), want invalid with %q", err, ops.KindOf(err), tc.want)
			}
			for _, call := range calls(fixture) {
				if strings.HasPrefix(call, "POST") {
					t.Fatalf("created a PR despite the bad reviewer: %v", calls(fixture))
				}
			}
		})
	}
}

func TestEditReviewersByName(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	_, err := invoke(t, pr.Edit, env, `{"repo":"acme/widgets","pr":"42","add_reviewers":["Margaret Hamilton"],"remove_reviewers":["ken thompson","linus"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := calls(fixture)
	body := got[len(got)-1]
	if !strings.Contains(body, `"account_id":"557058:grace"`) || !strings.Contains(body, `"account_id":"557058:mentioned"`) ||
		strings.Contains(body, "557058:ken") || strings.Contains(body, "557058:linus") {
		t.Fatalf("reviewers payload = %s", body)
	}

	env, fixture = newTestEnv(t, "")
	_, err = invoke(t, pr.Edit, env, `{"repo":"acme/widgets","pr":"42","remove_reviewers":["Ada Lovelace"]}`)
	if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), `no reviewer of this pull request is named "Ada Lovelace"`) {
		t.Fatalf("err = %v", err)
	}
	for _, call := range calls(fixture) {
		if strings.HasPrefix(call, "PUT") {
			t.Fatal("edited despite the unknown reviewer")
		}
	}
}
