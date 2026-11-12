package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdatePullRequestComment(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"id":12,"content":{"raw":"edited"}}`)
	})

	comment, err := client.UpdatePullRequestComment(context.Background(), "ws", "repo", 7, 12, "edited")
	if err != nil {
		t.Fatalf("UpdatePullRequestComment() error = %v", err)
	}
	if comment.ID != 12 || comment.Content.Raw != "edited" {
		t.Fatalf("comment = %#v", comment)
	}

	req := recorder.all()[0]
	expectRequest(t, req, http.MethodPut, "/repositories/ws/repo/pullrequests/7/comments/12")
	if string(req.Body) != `{"content":{"raw":"edited"}}` {
		t.Fatalf("body = %s", req.Body)
	}
}

func TestDeletePullRequestComment(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path == "/repositories/ws/repo/pullrequests/7/comments/13" {
			writeJSON(w, http.StatusForbidden, `{"type":"error","error":{"message":"You can only delete your own comments"}}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.DeletePullRequestComment(context.Background(), "ws", "repo", 7, 12); err != nil {
		t.Fatalf("DeletePullRequestComment() error = %v", err)
	}
	req := recorder.all()[0]
	expectRequest(t, req, http.MethodDelete, "/repositories/ws/repo/pullrequests/7/comments/12")
	if len(req.Body) != 0 {
		t.Fatalf("body = %s, want empty", req.Body)
	}

	err := client.DeletePullRequestComment(context.Background(), "ws", "repo", 7, 13)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden || apiErr.Message != "You can only delete your own comments" {
		t.Fatalf("error = %v, want Bitbucket message", err)
	}
}

func TestUpdatePullRequestDestination(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"id":7,"destination":{"branch":{"name":"release"}}}`)
	})

	pr, err := client.UpdatePullRequest(context.Background(), "ws", "repo", 7, PullRequestUpdate{
		Destination: &PullRequestRefInput{Branch: Branch{Name: "release"}},
	})
	if err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	if pr.Destination.Branch.Name != "release" {
		t.Fatalf("pr.Destination = %#v", pr.Destination)
	}
	req := recorder.all()[0]
	expectRequest(t, req, http.MethodPut, "/repositories/ws/repo/pullrequests/7")
	if string(req.Body) != `{"destination":{"branch":{"name":"release"}}}` {
		t.Fatalf("body = %s", req.Body)
	}

	title := "Only title"
	if _, err := client.UpdatePullRequest(context.Background(), "ws", "repo", 7, PullRequestUpdate{Title: &title}); err != nil {
		t.Fatalf("UpdatePullRequest(title) error = %v", err)
	}
	if body := string(recorder.all()[1].Body); body != `{"title":"Only title"}` {
		t.Fatalf("title-only body = %s, want no destination", body)
	}
}

func TestClientOptionsOverrideBaseURLAndHTTPClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path = %s, want /user", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, `{"account_id":"me"}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClientWithCredentials("user", "token",
		WithBaseURL(server.URL+"/"),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClientWithCredentials() error = %v", err)
	}
	if client.baseURL != server.URL || client.httpClient != server.Client() {
		t.Fatalf("client baseURL = %s httpClient = %p", client.baseURL, client.httpClient)
	}
	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentUser() error = %v", err)
	}
	if user.AccountID != "me" {
		t.Fatalf("user = %#v", user)
	}
}
