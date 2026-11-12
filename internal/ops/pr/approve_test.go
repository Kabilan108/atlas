package pr_test

import (
	"encoding/json"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestApprovalOps(t *testing.T) {
	tests := []struct {
		def     ops.Def
		args    string
		receipt string
		calls   []string
	}{
		{pr.Approve, `"body":"lgtm"`, `{"pull_request":42,"state":"approved","approved":true}`,
			[]string{"GET " + prPath, "POST " + prPath + "/comments", `  {"content":{"raw":"lgtm"}}`, "POST " + prPath + "/approve"}},
		{pr.RequestChanges, `"pr":"42"`, `{"pull_request":42,"state":"changes_requested","approved":false}`,
			[]string{"GET " + prPath, "POST " + prPath + "/request-changes"}},
		{pr.Unapprove, `"pr":"42"`, `{"approved":false,"pull_request":42}`,
			[]string{"GET " + prPath, "DELETE " + prPath + "/approve"}},
		{pr.ClearChangeRequest, `"pr":"42"`, `{"changes_requested":false,"pull_request":42}`,
			[]string{"GET " + prPath, "DELETE " + prPath + "/request-changes"}},
	}
	for _, tc := range tests {
		t.Run(tc.def.Info().Name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			result, err := invoke(t, tc.def, env, `{"repo":"acme/widgets","pr":"42",`+tc.args+`}`)
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != tc.receipt {
				t.Fatalf("receipt = %s, want %s", result.Text, tc.receipt)
			}
			wantCalls(t, fixture, tc.calls...)
			if meta := tc.def.Info(); meta.ReadOnly || meta.Destructive || meta.Surfaces != ops.Both {
				t.Fatalf("meta = %+v", meta)
			}
		})
	}
}

func TestApproveJSONIsTheParticipant(t *testing.T) {
	env, _ := newTestEnv(t, "")
	result, err := pr.Approve.Call(t.Context(), env, pr.ApproveInput{Ref: ref42()})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || fields["state"] != "approved" || fields["role"] != "REVIEWER" || len(fields) != 6 {
		t.Fatalf("JSON = %s", data)
	}
}
