package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestListPullRequestStatusesFollowsPagination(t *testing.T) {
	t.Parallel()

	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, `{"values":[{"key":"lint","state":"FAILED","refname":"feature"}]}`)
			return
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"next":"%s/repositories/ws/repo/pullrequests/7/statuses?page=2","values":[{"key":"build","name":"Build","state":"SUCCESSFUL","url":"https://ci/1","description":"ok","updated_on":"2026-01-02T03:04:05Z"}]}`, serverURL))
	})
	serverURL = server.URL

	statuses, err := client.ListPullRequestStatuses(context.Background(), "ws", "repo", 7)
	if err != nil {
		t.Fatalf("ListPullRequestStatuses() error = %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	first := statuses[0]
	if first.Key != "build" || first.Name != "Build" || first.State != CommitStatusSuccessful || first.URL != "https://ci/1" || first.Description != "ok" || first.UpdatedOn.IsZero() {
		t.Fatalf("statuses[0] = %#v", first)
	}
	if statuses[1].State != CommitStatusFailed || statuses[1].RefName != "feature" {
		t.Fatalf("statuses[1] = %#v", statuses[1])
	}

	requests := recorder.all()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	expectRequest(t, requests[0], http.MethodGet, "/repositories/ws/repo/pullrequests/7/statuses")
	if requests[1].Query.Get("page") != "2" {
		t.Fatalf("second request query = %v, want page=2", requests[1].Query)
	}
}

func TestListCommitStatusesUsesCommitPath(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"values":[]}`)
	})

	statuses, err := client.ListCommitStatuses(context.Background(), "ws", "repo", "abc123")
	if err != nil {
		t.Fatalf("ListCommitStatuses() error = %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses = %#v, want none", statuses)
	}
	expectRequest(t, recorder.all()[0], http.MethodGet, "/repositories/ws/repo/commit/abc123/statuses")

	if _, err := client.ListCommitStatuses(context.Background(), "ws", "repo", ""); err == nil {
		t.Fatal("ListCommitStatuses(empty) error = nil, want error")
	}
}
