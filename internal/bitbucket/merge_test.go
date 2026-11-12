package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestMergePullRequestSendsParametersAndParses200(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"id":7,"state":"MERGED","merge_commit":{"hash":"deadbeef"}}`)
	})

	result, err := client.MergePullRequest("ws", "repo", 7, PullRequestMergeOptions{
		Strategy:          MergeStrategySquash,
		Message:           "Squashed",
		CloseSourceBranch: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if result.Pending() || result.PullRequest == nil || result.PullRequest.State != "MERGED" {
		t.Fatalf("result = %#v, want merged PR", result)
	}
	if result.PullRequest.MergeCommit == nil || result.PullRequest.MergeCommit.Hash != "deadbeef" {
		t.Fatalf("merge commit = %#v, want deadbeef", result.PullRequest.MergeCommit)
	}

	req := recorder.all()[0]
	expectRequest(t, req, http.MethodPost, "/repositories/ws/repo/pullrequests/7/merge")
	if req.Query.Get("async") != "" {
		t.Fatalf("query = %v, want no async", req.Query)
	}
	body := decodeBody(t, req.Body)
	want := map[string]any{
		"type":                "pullrequest_merge_parameters",
		"message":             "Squashed",
		"close_source_branch": true,
		"merge_strategy":      "squash",
	}
	for key, value := range want {
		if body[key] != value {
			t.Fatalf("body[%s] = %#v, want %#v (body %s)", key, body[key], value, req.Body)
		}
	}
}

func TestMergePullRequestOmitsUnsetParameters(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"id":7,"state":"MERGED"}`)
	})

	if _, err := client.MergePullRequest("ws", "repo", 7, PullRequestMergeOptions{Async: true}); err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	req := recorder.all()[0]
	if req.Query.Get("async") != "true" {
		t.Fatalf("query = %v, want async=true", req.Query)
	}
	body := decodeBody(t, req.Body)
	for _, key := range []string{"merge_strategy", "message", "close_source_branch"} {
		if _, ok := body[key]; ok {
			t.Fatalf("body has %s, want omitted: %s", key, req.Body)
		}
	}
}

func TestMergePullRequestRejectsUnknownStrategy(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{}`)
	})

	_, err := client.MergePullRequest("ws", "repo", 7, PullRequestMergeOptions{Strategy: "octopus"})
	if err == nil || !strings.Contains(err.Error(), "octopus") {
		t.Fatalf("error = %v, want unknown strategy", err)
	}
	if len(recorder.all()) != 0 {
		t.Fatal("request sent for invalid strategy")
	}
}

func TestMergePullRequestPassesBitbucketMessageThrough(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusBadRequest, http.StatusConflict} {
		client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, status, `{"type":"error","error":{"message":"You can't merge until you resolve all merge checks."}}`)
		})

		_, err := client.MergePullRequest("ws", "repo", 7, PullRequestMergeOptions{})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: error = %v, want APIError", status, err)
		}
		if apiErr.StatusCode != status || apiErr.Message != "You can't merge until you resolve all merge checks." {
			t.Fatalf("status %d: apiErr = %#v", status, apiErr)
		}
	}
}

func TestMergePullRequest202PollsTaskStatusToSuccess(t *testing.T) {
	t.Parallel()

	var polls atomic.Int32
	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodPost:
			w.Header().Set("Location", serverURL+"/repositories/ws/repo/pullrequests/7/merge/task-status/task-1")
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/repositories/ws/repo/pullrequests/7/merge/task-status/task-1":
			if polls.Add(1) < 3 {
				writeJSON(w, http.StatusOK, `{"task_status":"PENDING"}`)
				return
			}
			writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS","merge_result":{"id":7,"state":"MERGED","merge_commit":{"hash":"cafe"}}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	})
	serverURL = server.URL

	result, err := client.MergePullRequest("ws", "repo", 7, PullRequestMergeOptions{})
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if !result.Pending() || !strings.HasSuffix(result.TaskLocation, "/merge/task-status/task-1") {
		t.Fatalf("result = %#v, want pending task", result)
	}

	pr, err := client.WaitForMerge(context.Background(), result.TaskLocation, time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatalf("WaitForMerge() error = %v", err)
	}
	if pr == nil || pr.State != "MERGED" {
		t.Fatalf("pr = %#v, want merged", pr)
	}
	if pr.MergeCommit == nil || pr.MergeCommit.Hash != "cafe" {
		t.Fatalf("merge commit = %#v, want cafe", pr.MergeCommit)
	}
	if got := polls.Load(); got != 3 {
		t.Fatalf("polls = %d, want 3", got)
	}
	for _, req := range recorder.all()[1:] {
		if req.Method != http.MethodGet {
			t.Fatalf("poll method = %s, want GET", req.Method)
		}
		if user, _, ok := (&http.Request{Header: req.Header}).BasicAuth(); !ok || user != "user" {
			t.Fatal("poll request missing credentials")
		}
	}
}

func TestWaitForMergeStopsAtDeadline(t *testing.T) {
	t.Parallel()

	var polls atomic.Int32
	client, _, server := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		polls.Add(1)
		writeJSON(w, http.StatusOK, `{"task_status":"PENDING"}`)
	})

	location := server.URL + "/repositories/ws/repo/pullrequests/7/merge/task-status/slow"
	start := time.Now()
	_, err := client.WaitForMerge(context.Background(), location, start.Add(350*time.Millisecond))
	if !errors.Is(err, ErrMergePending) {
		t.Fatalf("error = %v, want ErrMergePending", err)
	}
	if !strings.Contains(err.Error(), location) {
		t.Fatalf("error = %v, want task location", err)
	}
	if elapsed := time.Since(start); elapsed < 350*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("elapsed = %s, want about the deadline", elapsed)
	}
	if polls.Load() < 2 {
		t.Fatalf("polls = %d, want at least 2", polls.Load())
	}
}

func TestWaitForMergeHonoursContextAndFailures(t *testing.T) {
	t.Parallel()

	client, _, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if strings.HasSuffix(r.URL.Path, "/failed") {
			writeJSON(w, http.StatusConflict, `{"type":"error","error":{"message":"Merge conflict"}}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"task_status":"PENDING"}`)
	})
	base := server.URL + "/repositories/ws/repo/pullrequests/7/merge/task-status/"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.WaitForMerge(ctx, base+"pending", time.Now().Add(time.Minute)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v, want context.Canceled", err)
	}

	_, err := client.WaitForMerge(context.Background(), base+"failed", time.Now().Add(time.Minute))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Merge conflict" {
		t.Fatalf("failed error = %v, want Bitbucket message", err)
	}
}

func TestWaitForMergeRefusesForeignLocation(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS"}`)
	})

	_, err := client.WaitForMerge(context.Background(), "https://evil.example/2.0/task-status/1", time.Now().Add(time.Second))
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error = %v, want refusal", err)
	}
	if len(recorder.all()) != 0 {
		t.Fatal("request sent to API for foreign location")
	}
}
