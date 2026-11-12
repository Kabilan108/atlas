package bitbucket

import (
	"context"
	"net/http"
	"testing"
)

// Workspace and repo values must stay inside their path segments: a "?" or
// "#" in a repo once turned the rest of the value into a query string.
func TestRepositoryPathSegmentsAreEscaped(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"id":1,"values":[{"id":1}]}`)
	})
	ctx := context.Background()
	calls := []struct {
		name string
		call func() error
		want string
	}{
		{"get pr", func() error {
			_, err := client.GetPullRequest(ctx, "acme", "widgets?fields=description#", 1)
			return err
		}, "/repositories/acme/widgets%3Ffields=description%23/pullrequests/1"},
		{"repository", func() error {
			_, err := client.GetRepository(ctx, "ac/me", "w%20x")
			return err
		}, "/repositories/ac%2Fme/w%2520x"},
		{"list repositories", func() error {
			_, err := client.ListRepositories(ctx, "acme?x=1")
			return err
		}, "/repositories/acme%3Fx=1"},
		{"comments", func() error {
			_, err := client.ListPullRequestComments(ctx, "acme", "a#b", 2)
			return err
		}, "/repositories/acme/a%23b/pullrequests/2/comments"},
	}
	for i, tc := range calls {
		if err := tc.call(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := recorder.all()[i]
		if got.EscapedPath != tc.want || len(got.Query) != 0 {
			t.Errorf("%s: path = %s query = %v, want %s and no query", tc.name, got.EscapedPath, got.Query, tc.want)
		}
	}
}

func TestBranchAndStateQueriesAreEncoded(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"values":[{"id":1,"state":"OPEN"}]}`)
	})
	ctx := context.Background()
	if _, err := client.FindPullRequestByBranch(ctx, "acme", "widgets", `fix/"quote"&state=MERGED`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListPullRequests(ctx, "acme", "widgets", &PRListOptions{State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	requests := recorder.all()
	if got := requests[0].Query; len(got) != 1 || got.Get("q") != `source.branch.name="fix/\"quote\"&state=MERGED"` {
		t.Fatalf("branch query = %v", got)
	}
	if got := requests[1].Query; len(got) != 1 || got.Get("state") != "OPEN" {
		t.Fatalf("list query = %v", got)
	}
}
