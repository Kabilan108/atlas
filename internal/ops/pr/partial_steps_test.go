package pr_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

// A comment-then-action write whose action fails has still posted the
// comment, so its receipt must say so and name the failed step; a retry can
// then leave the comment out instead of posting it twice.
func TestCommentThenActionFailureKeepsTheComment(t *testing.T) {
	tests := []struct {
		def     ops.Def
		args    string
		action  string
		step    string
		wantErr string
	}{
		{pr.Approve, `"body":"lgtm"`, "/approve", "approve", "posted comment but failed to approve pull request: "},
		{pr.RequestChanges, `"body":"fix it"`, "/request-changes", "request-changes", "posted comment but failed to request changes on pull request: "},
		{pr.Close, `"body":"bye"`, "/decline", "decline", "posted closing comment but failed to close pull request: "},
	}
	for _, tc := range tests {
		t.Run(tc.def.Info().Name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42"+tc.action, 0)
			result, err := invoke(t, tc.def, env, `{"repo":"acme/widgets","pr":"42",`+tc.args+`}`)
			if ops.KindOf(err) != ops.Partial || !strings.HasPrefix(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v (%v), want partial starting %q", err, ops.KindOf(err), tc.wantErr)
			}
			var receipt struct {
				Failed struct {
					PullRequest int    `json:"pull_request"`
					Step        string `json:"step"`
					Error       string `json:"error"`
					Comment     struct {
						ID  int    `json:"id"`
						URL string `json:"url"`
					} `json:"comment"`
				} `json:"failed"`
			}
			if err := json.Unmarshal([]byte(result.Text), &receipt); err != nil {
				t.Fatalf("receipt %q: %v", result.Text, err)
			}
			failed := receipt.Failed
			if failed.PullRequest != 42 || failed.Step != tc.step || !strings.Contains(failed.Error, "injected failure") ||
				failed.Comment.ID == 0 || !strings.HasPrefix(failed.Comment.URL, "https://bitbucket.org/acme/widgets/pull-requests/42#comment-") {
				t.Fatalf("receipt = %s", result.Text)
			}
			data, err := json.Marshal(result.Value)
			if err != nil || !strings.Contains(string(data), `"failed":{`) {
				t.Fatalf("value JSON = %s, %v", data, err)
			}
		})
	}
}

func TestActionFailureWithoutCommentIsNotPartial(t *testing.T) {
	tests := []struct {
		def    ops.Def
		action string
	}{
		{pr.Approve, "/approve"},
		{pr.RequestChanges, "/request-changes"},
		{pr.Close, "/decline"},
	}
	for _, tc := range tests {
		t.Run(tc.def.Info().Name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			fixture.Fail(http.MethodPost, "/repositories/acme/widgets/pullrequests/42"+tc.action, 0)
			result, err := invoke(t, tc.def, env, `{"repo":"acme/widgets","pr":"42"}`)
			if err == nil || ops.KindOf(err) != ops.Upstream || result.Text != "" || strings.HasPrefix(err.Error(), "posted") {
				t.Fatalf("result = %+v, err = %v (%v)", result, err, ops.KindOf(err))
			}
		})
	}
}
