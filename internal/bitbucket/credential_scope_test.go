package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type hostRecorder struct {
	mu    sync.Mutex
	hosts []string
	auth  map[string]bool
}

func (r *hostRecorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.auth == nil {
		r.auth = map[string]bool{}
	}
	r.hosts = append(r.hosts, req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
	if req.Header.Get("Authorization") != "" {
		r.auth[req.URL.Host] = true
	}
}

func (r *hostRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hosts...)
}

func (r *hostRecorder) sentAuthTo(host string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.auth[host]
}

func cannedResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// fakeAPIClient serves page one under the base URL with the given next link,
// and answers anything else with a second page so a followed link is visible.
func fakeAPIClient(baseURL, next string) (*Client, *hostRecorder) {
	recorder := &hostRecorder{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder.record(req)
		if len(recorder.all()) == 1 {
			return cannedResponse(req, http.StatusOK, `{"values":[{"name":"one"}],"next":`+strconv.Quote(next)+`}`), nil
		}
		return cannedResponse(req, http.StatusOK, `{"values":[{"name":"two"}]}`), nil
	})
	return newClientForTest(baseURL, &http.Client{Transport: transport}), recorder
}

func TestPaginationRefusesNextLinksOutsideBaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		baseURL string
		next    string
		host    string
	}{
		{"host suffix", "https://api.proxy.example", "https://api.proxy.example.attacker.test/repositories/ws?page=2", "api.proxy.example.attacker.test"},
		{"sibling path", "https://proxy.example/bitbucket", "https://proxy.example/bitbucket-evil/repositories/ws?page=2", "proxy.example"},
		{"other port", "https://api.proxy.example", "https://api.proxy.example:8443/repositories/ws?page=2", "api.proxy.example:8443"},
		{"other scheme", "https://api.proxy.example", "http://api.proxy.example/repositories/ws?page=2", "api.proxy.example"},
		{"user info", "https://api.proxy.example", "https://evil@api.proxy.example/repositories/ws?page=2", "api.proxy.example"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/collectPages", func(t *testing.T) {
			t.Parallel()
			client, recorder := fakeAPIClient(tc.baseURL, tc.next)
			_, err := client.SearchRepositories(context.Background(), "ws", nil)
			if err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Fatalf("error = %v, want refusal", err)
			}
			if got := recorder.all(); len(got) != 1 {
				t.Fatalf("requests = %v, want only the first page", got)
			}
		})
		t.Run(tc.name+"/legacy", func(t *testing.T) {
			t.Parallel()
			client, recorder := fakeAPIClient(tc.baseURL, tc.next)
			repos, err := client.ListRepositories(context.Background(), "ws")
			if err != nil {
				t.Fatalf("ListRepositories() error = %v", err)
			}
			if got := recorder.all(); len(got) != 1 || len(repos) != 1 {
				t.Fatalf("requests = %v, repos = %d, want only the first page", got, len(repos))
			}
		})
	}
}

func TestPaginationFollowsNextLinksUnderBasePath(t *testing.T) {
	t.Parallel()

	client, recorder := fakeAPIClient("https://proxy.example/bitbucket", "https://proxy.example/bitbucket/repositories/ws?page=2")
	repos, err := client.SearchRepositories(context.Background(), "ws", nil)
	if err != nil {
		t.Fatalf("SearchRepositories() error = %v", err)
	}
	got := recorder.all()
	if len(repos) != 2 || len(got) != 2 || got[1] != "https://proxy.example/bitbucket/repositories/ws" {
		t.Fatalf("requests = %v, repos = %d, want two pages under the base path", got, len(repos))
	}
}

func TestCollectPagesStopsWithoutNextLink(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if hits.Add(1) > 5 {
			writeJSON(w, http.StatusInternalServerError, `{}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"page":1,"pagelen":2,"size":5,"values":[{"build_number":5},{"build_number":4}]}`)
	})

	pipelines, err := client.ListPipelines(context.Background(), "ws", "repo", &PipelineListOptions{Limit: 10, PageLen: 2})
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 2 || hits.Load() != 1 {
		t.Fatalf("pipelines = %d, requests = %d, want one page", len(pipelines), hits.Load())
	}
}

func TestCollectPagesRejectsRepeatedNextLink(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	var serverURL string
	client, _, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if hits.Add(1) > 5 {
			writeJSON(w, http.StatusInternalServerError, `{}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"values":[{"name":"main"}],"next":`+strconv.Quote(serverURL+r.URL.RequestURI())+`}`)
	})
	serverURL = server.URL

	_, err := client.ListBranches(context.Background(), "ws", "repo", nil)
	if err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("error = %v, want non-advancing pagination error", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}
}

// newRedirectingAPI returns an API server whose every response redirects to a
// second server on the same host but another port. Go's default redirect
// policy forwards Authorization there, since it compares hostnames only.
func newRedirectingAPI(t *testing.T) (api, foreign *httptest.Server, foreignAuth *atomic.Value) {
	t.Helper()
	foreignAuth = &atomic.Value{}
	foreignAuth.Store("")
	foreign = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignAuth.Store(r.Header.Get("Authorization"))
		writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS","name":"main","target":{"hash":"abc"},"values":[]}`)
	}))
	t.Cleanup(foreign.Close)
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", foreign.URL+r.URL.Path)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(api.Close)
	return api, foreign, foreignAuth
}

func TestAuthenticatedRequestsRefuseRedirectsOffAPIOrigin(t *testing.T) {
	t.Parallel()

	calls := map[string]func(c *Client, apiURL string) error{
		"WaitForMerge": func(c *Client, apiURL string) error {
			_, err := c.WaitForMerge(context.Background(), apiURL+"/repositories/ws/repo/pullrequests/7/merge/task-status/1", time.Now().Add(5*time.Second))
			return err
		},
		"GetBranch": func(c *Client, _ string) error {
			_, err := c.GetBranch(context.Background(), "ws", "repo", "main")
			return err
		},
		"GetFileContent": func(c *Client, _ string) error {
			_, err := c.GetFileContent(context.Background(), "ws", "repo", "main", "README.md")
			return err
		},
		"GetPullRequestDiff": func(c *Client, _ string) error {
			_, err := c.GetPullRequestDiff(context.Background(), "ws", "repo", 7)
			return err
		},
		"ListRepositories": func(c *Client, _ string) error {
			_, err := c.ListRepositories(context.Background(), "ws")
			return err
		},
	}
	clients := map[string]func(api *httptest.Server) *http.Client{
		"default": func(api *httptest.Server) *http.Client { return api.Client() },
		"injected permissive": func(*httptest.Server) *http.Client {
			return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
		},
	}

	for callName, call := range calls {
		for clientName, newHTTPClient := range clients {
			t.Run(callName+"/"+clientName, func(t *testing.T) {
				t.Parallel()
				api, _, foreignAuth := newRedirectingAPI(t)
				client := newClientForTest(api.URL, nil)
				WithHTTPClient(newHTTPClient(api))(client)

				err := call(client, api.URL)
				if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
					t.Fatalf("error = %v, want redirect refusal", err)
				}
				if auth := foreignAuth.Load().(string); auth != "" {
					t.Fatalf("foreign origin received Authorization %q", auth)
				}
			})
		}
	}
}

func TestAuthenticatedRequestsFollowRedirectsWithinBaseURL(t *testing.T) {
	t.Parallel()

	var redirectedAuth atomic.Value
	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path == "/repositories/ws/repo/refs/branches/main" {
			http.Redirect(w, r, "/repositories/ws/repo/refs/branches/trunk", http.StatusFound)
			return
		}
		redirectedAuth.Store(r.Header.Get("Authorization"))
		writeJSON(w, http.StatusOK, `{"name":"trunk","target":{"hash":"abc"}}`)
	})

	branch, err := client.GetBranch(context.Background(), "ws", "repo", "main")
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if branch.Name != "trunk" || !strings.HasPrefix(redirectedAuth.Load().(string), "Basic ") {
		t.Fatalf("branch = %#v, auth = %v, want followed same-origin redirect", branch, redirectedAuth.Load())
	}
}

func TestWaitForMergeAbortsInFlightPollOnCancel(t *testing.T) {
	t.Parallel()

	client, _, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
		writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS","merge_result":{"id":7,"state":"MERGED"}}`)
	})
	location := server.URL + "/repositories/ws/repo/pullrequests/7/merge/task-status/slow"

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	pr, err := client.WaitForMerge(ctx, location, time.Now().Add(time.Minute))
	if !errors.Is(err, context.Canceled) || pr != nil {
		t.Fatalf("pr = %#v, error = %v, want context.Canceled", pr, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want the in-flight poll aborted", elapsed)
	}

	start = time.Now()
	pr, err = client.WaitForMerge(context.Background(), location, time.Now().Add(150*time.Millisecond))
	if !errors.Is(err, ErrMergePending) || pr != nil {
		t.Fatalf("pr = %#v, error = %v, want ErrMergePending", pr, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want the in-flight poll aborted at the deadline", elapsed)
	}
}

func TestWaitForMergeChecksContextBeforePolling(t *testing.T) {
	t.Parallel()

	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS","merge_result":{"id":7,"state":"MERGED"}}`)
	})
	location := server.URL + "/repositories/ws/repo/pullrequests/7/merge/task-status/1"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pr, err := client.WaitForMerge(ctx, location, time.Now().Add(time.Minute)); !errors.Is(err, context.Canceled) || pr != nil {
		t.Fatalf("cancelled: pr = %#v, error = %v, want context.Canceled", pr, err)
	}
	if pr, err := client.WaitForMerge(context.Background(), location, time.Now().Add(-time.Second)); !errors.Is(err, ErrMergePending) || pr != nil {
		t.Fatalf("expired: pr = %#v, error = %v, want ErrMergePending", pr, err)
	}
	if got := len(recorder.all()); got != 0 {
		t.Fatalf("requests = %d, want none after cancellation or expiry", got)
	}
}

func TestWaitForMergeRateLimitRetryHonoursDeadline(t *testing.T) {
	t.Parallel()

	client, _, server := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(5*time.Second).Unix(), 10))
		writeJSON(w, http.StatusTooManyRequests, `{}`)
	})
	client.retry = true
	location := server.URL + "/repositories/ws/repo/pullrequests/7/merge/task-status/1"

	start := time.Now()
	_, err := client.WaitForMerge(context.Background(), location, time.Now().Add(200*time.Millisecond))
	if !errors.Is(err, ErrMergePending) {
		t.Fatalf("error = %v, want ErrMergePending", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("elapsed = %s, want the rate-limit wait cut at the deadline", elapsed)
	}
}
