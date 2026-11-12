package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestListAllPullRequestsReportsFailedRepositories(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/repositories/ws":
			writeJSON(w, http.StatusOK, `{"values":[
				{"name":"Open Repo","full_name":"ws/open-repo"},
				{"name":"secret","full_name":"ws/secret"},
				{"name":"gone","full_name":"ws/gone"}]}`)
		case "/repositories/ws/open-repo/pullrequests":
			writeJSON(w, http.StatusOK, `{"values":[{"id":1,"title":"one"}]}`)
		case "/repositories/ws/secret/pullrequests":
			writeJSON(w, http.StatusForbidden, `{"type":"error","error":{"message":"no access"}}`)
		default:
			writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"missing"}}`)
		}
	})

	prs, err := client.ListAllPullRequests(context.Background(), "ws", nil)
	if len(prs) != 1 || prs[0].ID != 1 {
		t.Fatalf("prs = %+v, want the open repository's PR", prs)
	}
	var listErr *RepositoryListError
	if !errors.As(err, &listErr) {
		t.Fatalf("error = %v, want *RepositoryListError", err)
	}
	if listErr.Repositories != 3 || len(listErr.Failed) != 2 ||
		listErr.Failed[0].Repository != "secret" || listErr.Failed[1].Repository != "gone" {
		t.Fatalf("listErr = %+v", listErr)
	}
	var apiErr *APIError
	if !errors.As(listErr.Failed[0].Err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("secret error = %v, want the 403", listErr.Failed[0].Err)
	}
	text := err.Error()
	if !strings.HasPrefix(text, "failed to list pull requests in 2 of 3 repositories:") ||
		!strings.Contains(text, "\n  secret: authentication: access denied") ||
		!strings.Contains(text, "\n  gone: ") || strings.Contains(text, "Hint:") {
		t.Fatalf("error text = %q", text)
	}
	if got := recorder.all()[1].Path; got != "/repositories/ws/open-repo/pullrequests" {
		t.Fatalf("listed %s, want the slug from full_name, not the display name", got)
	}
}

func TestListAllPullRequestsSucceedsWithoutFailures(t *testing.T) {
	t.Parallel()

	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path == "/repositories/ws" {
			writeJSON(w, http.StatusOK, `{"values":[{"name":"a","full_name":"ws/a"}]}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"values":[{"id":2}]}`)
	})
	prs, err := client.ListAllPullRequests(context.Background(), "ws", nil)
	if err != nil || len(prs) != 1 {
		t.Fatalf("prs = %+v, err = %v", prs, err)
	}
}
