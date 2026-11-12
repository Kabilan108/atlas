package bitbucket

import (
	"fmt"
	"net/http"
	"testing"
)

func TestListWorkspaceMembersFiltersByName(t *testing.T) {
	t.Parallel()

	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, `{"values":[{"user":{"display_name":"Janet Roe","account_id":"acct-3"}}]}`)
			return
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"next":"%s/workspaces/ws/members?page=2","values":[{"user":{"display_name":"Jane Doe","account_id":"acct-1","uuid":"{u1}"}},{"user":{"display_name":"Bob","nickname":"bjones","account_id":"acct-2"}}]}`, serverURL))
	})
	serverURL = server.URL

	users, err := client.ListWorkspaceMembers("ws", &MemberListOptions{Name: "JAN"})
	if err != nil {
		t.Fatalf("ListWorkspaceMembers() error = %v", err)
	}
	if len(users) != 2 || users[0].AccountID != "acct-1" || users[1].AccountID != "acct-3" {
		t.Fatalf("users = %#v", users)
	}

	byNickname, err := client.ListWorkspaceMembers("ws", &MemberListOptions{Name: "bjon"})
	if err != nil {
		t.Fatalf("ListWorkspaceMembers(nickname) error = %v", err)
	}
	if len(byNickname) != 1 || byNickname[0].AccountID != "acct-2" {
		t.Fatalf("byNickname = %#v", byNickname)
	}

	req := recorder.all()[0]
	expectRequest(t, req, http.MethodGet, "/workspaces/ws/members")
	if req.Query.Get("pagelen") != "100" || req.Query.Has("q") {
		t.Fatalf("query = %v", req.Query)
	}
}

func TestListWorkspaceMembersLimitWithoutFilter(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"next":"https://not-followed","values":[{"user":{"account_id":"a"}},{"user":{"account_id":"b"}}]}`)
	})

	users, err := client.ListWorkspaceMembers("ws", &MemberListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListWorkspaceMembers() error = %v", err)
	}
	if len(users) != 1 || len(recorder.all()) != 1 {
		t.Fatalf("users = %#v requests = %d", users, len(recorder.all()))
	}
}

func TestListPullRequestActivityDecodesKinds(t *testing.T) {
	t.Parallel()

	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Query().Get("ctx") == "next" {
			writeJSON(w, http.StatusOK, `{"values":[
				{"changes_requested":{"date":"2026-01-01T00:00:00Z","user":{"account_id":"r2"}}},
				{"pull_request":{"id":7},"something_new":{"date":"2026-01-01T00:00:00Z"}}
			]}`)
			return
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"next":"%s/repositories/ws/repo/pullrequests/7/activity?ctx=next","values":[
			{"approval":{"date":"2026-01-03T00:00:00Z","user":{"account_id":"r1"}},"pull_request":{"id":7}},
			{"update":{"state":"OPEN","title":"Retitled","date":"2026-01-02T12:00:00Z","author":{"account_id":"a1"},"source":{"commit":{"hash":"new"}},"changes":{"title":{"old":"Old","new":"Retitled"}}}},
			{"comment":{"id":9,"content":{"raw":"hi"},"user":{"account_id":"c1"},"created_on":"2026-01-02T00:00:00Z"}}
		]}`, serverURL))
	})
	serverURL = server.URL

	activity, err := client.ListPullRequestActivity("ws", "repo", 7, 0)
	if err != nil {
		t.Fatalf("ListPullRequestActivity() error = %v", err)
	}
	wantKinds := []string{ActivityKindApproval, ActivityKindUpdate, ActivityKindComment, ActivityKindChangesRequested, ActivityKindUnknown}
	if len(activity) != len(wantKinds) {
		t.Fatalf("len(activity) = %d, want %d", len(activity), len(wantKinds))
	}
	wantActors := []string{"r1", "a1", "c1", "r2", ""}
	for i, entry := range activity {
		if entry.Kind() != wantKinds[i] {
			t.Fatalf("activity[%d].Kind() = %s, want %s", i, entry.Kind(), wantKinds[i])
		}
		if entry.Actor().AccountID != wantActors[i] {
			t.Fatalf("activity[%d].Actor() = %#v, want %s", i, entry.Actor(), wantActors[i])
		}
		if len(entry.Raw) == 0 {
			t.Fatalf("activity[%d].Raw is empty", i)
		}
		if wantKinds[i] != ActivityKindUnknown && entry.Time().IsZero() {
			t.Fatalf("activity[%d].Time() is zero", i)
		}
	}
	if activity[1].Update.Source.Commit.Hash != "new" || activity[1].Update.Changes["title"] == nil {
		t.Fatalf("update = %#v", activity[1].Update)
	}

	expectRequest(t, recorder.all()[0], http.MethodGet, "/repositories/ws/repo/pullrequests/7/activity")
	if recorder.all()[0].Query.Has("pagelen") {
		t.Fatalf("unlimited query = %v, want server default pagelen", recorder.all()[0].Query)
	}

	limited, err := client.ListPullRequestActivity("ws", "repo", 7, 2)
	if err != nil {
		t.Fatalf("ListPullRequestActivity(limit) error = %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want 2", len(limited))
	}
	if got := recorder.all()[2].Query.Get("pagelen"); got != "2" {
		t.Fatalf("limited pagelen = %q, want 2", got)
	}
}
