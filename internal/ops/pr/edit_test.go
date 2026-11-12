package pr_test

import (
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestEdit(t *testing.T) {
	tests := []struct {
		name, args, body string
	}{
		{"title", `"title":"New"`, `{"title":"New"}`},
		{"base", `"base":"release"`, `{"destination":{"branch":{"name":"release"}}}`},
		{"clear body", `"body":""`, `{"description":""}`},
		{"body normalized", `"body":"To-Do:\n- a"`, `{"description":"To-Do:\n\n- a"}`},
		{"ready", `"draft":false`, `{"draft":false}`},
		{"reviewers", `"add_reviewers":["557058:mentioned"],"remove_reviewers":["{ken-uuid}","557058:linus"]`,
			`{"reviewers":[{"uuid":"{grace-uuid}","username":"","nickname":"grace","display_name":"Grace Hopper","account_id":"557058:grace","links":{"self":{"href":""},"html":{"href":""},"avatar":{"href":""}}},{"uuid":"{margaret-uuid}","username":"","nickname":"margaret","display_name":"Margaret Hamilton","account_id":"557058:mentioned","links":{"self":{"href":""},"html":{"href":""},"avatar":{"href":""}}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			result, err := invoke(t, pr.Edit, env, `{"repo":"acme/widgets","pr":"42",`+tc.args+`}`)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Value.(*pr.EditResult).Changed {
				t.Fatal("Changed = false")
			}
			got := calls(fixture)
			if got[len(got)-2] != "PUT "+prPath || got[len(got)-1] != "  "+tc.body {
				t.Fatalf("requests = %v, want PUT with %s", got, tc.body)
			}
		})
	}
}

func TestEditWithoutChangesSendsNothing(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.Edit, env, `{"repo":"acme/widgets","pr":"42","title":"Fix login: handle 'empty' users","base":"main"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":42,"title":"Fix login: handle 'empty' users","state":"OPEN","source":"feature/login","destination":"main","url":"https://bitbucket.org/acme/widgets/pull-requests/42","changed":false}`; result.Text != want {
		t.Fatalf("receipt = %s\nwant      %s", result.Text, want)
	}
	wantCalls(t, fixture, "GET "+prPath)
}
