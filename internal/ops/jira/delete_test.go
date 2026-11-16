package jira

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

const worklogPath = "/rest/api/3/issue/CSM-756/worklog/"

func worklogJSON(id string) string {
	return `{"id":"` + id + `","issueId":"30756","author":{"accountId":"acc-me","displayName":"Tony"},` +
		`"started":"2026-11-11T09:00:00.000-0500","timeSpent":"1h 30m","timeSpentSeconds":5400}`
}

func TestWorklogDelete(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*jiratest.Fake)
		args  string
		// kind is the error kind; Unknown means success.
		kind ops.Kind
		// errHas are substrings of the error.
		errHas []string
		// want are substrings of the receipt text.
		want []string
		// deletes are the worklog IDs DELETEd, in order.
		deletes []string
	}{
		{
			name: "one worklog",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+worklogPath+"900", http.StatusOK, worklogJSON("900"))
				f.JSON("DELETE "+worklogPath+"900", http.StatusNoContent, "")
			},
			args:    `{"issue":"csm-756","ids":["900"]}`,
			want:    []string{`{"key":"CSM-756","results":[{"id":"900","status":"ok","author":"Tony","started":"2026-11-11T09:00:00.000-0500","time_spent":"1h 30m"}]}`},
			deletes: []string{"900"},
		},
		{
			name: "browse URL and time from seconds",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+worklogPath+"900", http.StatusOK, `{"id":"900","started":"2026-11-11T09:00:00.000-0500","timeSpentSeconds":2700}`)
				f.JSON("DELETE "+worklogPath+"900", http.StatusNoContent, "")
			},
			args:    `{"issue":"https://acme.atlassian.net/browse/CSM-756","ids":[" 900 "]}`,
			want:    []string{`"id":"900","status":"ok","started":"2026-11-11T09:00:00.000-0500","time_spent":"45m"`},
			deletes: []string{"900"},
		},
		{
			name: "batch with a missing worklog",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+worklogPath+"900", http.StatusOK, worklogJSON("900"))
				f.JSON("DELETE "+worklogPath+"900", http.StatusNoContent, "")
				f.JSON("GET "+worklogPath+"902", http.StatusOK, worklogJSON("902"))
				f.JSON("DELETE "+worklogPath+"902", http.StatusNoContent, "")
			},
			args:    `{"issue":"CSM-756","ids":["900","901","902"]}`,
			kind:    ops.Partial,
			errHas:  []string{"1 of 3 worklog deletes failed"},
			want:    []string{`"id":"900","status":"ok"`, `"id":"901","status":"failed","error":"worklog 901 not found on CSM-756, or you cannot see that issue`, `"id":"902","status":"ok"`},
			deletes: []string{"900", "902"},
		},
		{
			name:   "one missing worklog",
			args:   `{"issue":"CSM-756","ids":["901"]}`,
			kind:   ops.NotFound,
			errHas: []string{"worklog 901 not found on CSM-756"},
		},
		{
			name: "forbidden delete",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+worklogPath+"900", http.StatusOK, worklogJSON("900"))
				f.JSON("DELETE "+worklogPath+"900", http.StatusForbidden, `{"errorMessages":["You do not have the permission to delete this worklog."]}`)
			},
			args:    `{"issue":"CSM-756","ids":["900"]}`,
			kind:    ops.Auth,
			errHas:  []string{"worklog 900 on CSM-756", "Delete Own Worklogs", "You do not have the permission"},
			deletes: []string{"900"},
		},
		{name: "no IDs", args: `{"issue":"CSM-756","ids":[]}`, kind: ops.Invalid, errHas: []string{"1 to 100"}},
		{name: "path in an ID", args: `{"issue":"CSM-756","ids":["900/../901"]}`, kind: ops.Invalid, errHas: []string{`ids[0] "900/../901"`, "numeric worklog ID"}},
		{name: "repeated ID", args: `{"issue":"CSM-756","ids":["900","900"]}`, kind: ops.Invalid, errHas: []string{"ids[1]: worklog 900 is listed twice"}},
		{name: "too many IDs", args: `{"issue":"CSM-756","ids":[` + strings.Repeat(`"1",`, 100) + `"2"]}`, kind: ops.Invalid, errHas: []string{"1 to 100"}},
		{name: "bad issue", args: `{"issue":"CSM 756","ids":["900"]}`, kind: ops.Invalid, errHas: []string{"invalid issue"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := jiratest.New()
			if tt.setup != nil {
				tt.setup(fake)
			}
			res, err := invoke(t, fake, WorklogDelete, tt.args)
			if tt.kind == ops.Unknown {
				if err != nil {
					t.Fatalf("%v\nrequests: %v", err, fake.Routes())
				}
			} else {
				wantKind(t, err, tt.kind, tt.errHas...)
			}
			wantContains(t, res.Text, tt.want...)
			var deleted []string
			for _, r := range fake.Requests() {
				if r.Method != http.MethodDelete {
					continue
				}
				id, ok := strings.CutPrefix(r.Path, worklogPath)
				if !ok {
					t.Errorf("DELETE %s, want one under %s", r.Path, worklogPath)
				}
				if r.Query != "adjustEstimate=auto" {
					t.Errorf("DELETE %s query = %q, want adjustEstimate=auto", r.Path, r.Query)
				}
				deleted = append(deleted, id)
			}
			if strings.Join(deleted, ",") != strings.Join(tt.deletes, ",") {
				t.Errorf("deleted %v, want %v\nrequests: %v", deleted, tt.deletes, fake.Routes())
			}
		})
	}
}

const commentPath = "/rest/api/3/issue/CSM-756/comment/15198"

func TestCommentDelete(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*jiratest.Fake)
		args   string
		kind   ops.Kind
		errHas []string
		want   string
		// deleted says whether the DELETE was sent.
		deleted bool
	}{
		{
			name: "internal note",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+commentPath, http.StatusOK, `{"id":"15198","author":{"displayName":"Tony"},"created":"2026-11-12T10:00:00.000-0500","jsdPublic":false}`)
				f.JSON("DELETE "+commentPath, http.StatusNoContent, "")
			},
			args:    `{"issue":"CSM-756","id":"15198"}`,
			want:    `{"key":"CSM-756","id":"15198","author":"Tony","created":"2026-11-12T10:00:00.000-0500","internal":true}`,
			deleted: true,
		},
		{
			name: "comment outside a service desk",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+commentPath, http.StatusOK, `{"id":"15198","author":{"accountId":"acc-1"},"created":"2026-11-12T10:00:00.000-0500"}`)
				f.JSON("DELETE "+commentPath, http.StatusNoContent, "")
			},
			args:    `{"issue":"csm-756","id":"15198"}`,
			want:    `{"key":"CSM-756","id":"15198","author":"acc-1","created":"2026-11-12T10:00:00.000-0500","internal":false}`,
			deleted: true,
		},
		{
			name:   "missing comment",
			args:   `{"issue":"CSM-756","id":"15198"}`,
			kind:   ops.NotFound,
			errHas: []string{"comment 15198 not found on CSM-756"},
		},
		{
			name: "forbidden delete",
			setup: func(f *jiratest.Fake) {
				f.JSON("GET "+commentPath, http.StatusOK, `{"id":"15198","jsdPublic":true}`)
				f.JSON("DELETE "+commentPath, http.StatusForbidden, `{"errorMessages":["You do not have permission to delete this comment."]}`)
			},
			args:    `{"issue":"CSM-756","id":"15198"}`,
			kind:    ops.Auth,
			errHas:  []string{"comment 15198 on CSM-756", "Delete Own Comments"},
			deleted: true,
		},
		{name: "no ID", args: `{"issue":"CSM-756"}`, kind: ops.Invalid, errHas: []string{"numeric comment ID"}},
		{name: "path in the ID", args: `{"issue":"CSM-756","id":"1?x=1"}`, kind: ops.Invalid, errHas: []string{"numeric comment ID"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := jiratest.New()
			if tt.setup != nil {
				tt.setup(fake)
			}
			res, err := invoke(t, fake, CommentDelete, tt.args)
			if tt.kind == ops.Unknown {
				if err != nil {
					t.Fatalf("%v\nrequests: %v", err, fake.Routes())
				}
				if res.Text != tt.want {
					t.Errorf("receipt = %s, want %s", res.Text, tt.want)
				}
			} else {
				wantKind(t, err, tt.kind, tt.errHas...)
			}
			if got := len(fake.Find("DELETE " + commentPath)); got != map[bool]int{false: 0, true: 1}[tt.deleted] {
				t.Errorf("sent %d DELETEs, want deleted=%v\nrequests: %v", got, tt.deleted, fake.Routes())
			}
			for _, r := range fake.Requests() {
				if r.Method == http.MethodDelete && r.Path != commentPath {
					t.Errorf("unexpected DELETE %s", r.Path)
				}
			}
		})
	}
}
