// Package mergetest serves pull request #7 of acme/widgets with configurable
// build statuses and merge behaviour, for pr_checks and pr_merge tests.
package mergetest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const (
	PRID        = 7
	Head        = "abc123def456"
	MergeCommit = "fedcba9876543210fedcba9876543210fedcba98"
	URL         = "https://bitbucket.org/acme/widgets/pull-requests/7"
	// Host is the only host RoundTrip serves.
	Host = "api.bitbucket.org"

	prPath     = "/2.0/repositories/acme/widgets/pullrequests/7"
	taskPath   = prPath + "/merge/task-status/t1"
	statusPath = "/2.0/repositories/acme/widgets/commit/"
)

// MergeMode is how the fixture answers POST .../merge.
type MergeMode int

const (
	// MergeNow answers 200 with the merged PR.
	MergeNow MergeMode = iota
	// MergeQueued answers 202; the task succeeds after TaskPolls polls.
	MergeQueued
	// MergeStuck answers 202; the task stays PENDING.
	MergeStuck
	// MergeRefused answers 400 with a merge-check message.
	MergeRefused
	// MergeConflict answers 409.
	MergeConflict
)

// RefusalMessage is Bitbucket's reason in MergeRefused mode.
const RefusalMessage = "You can't merge until you have at least 1 approval."

// Fixture is the fake API. HeadCommit is the PR's source commit, Head unless
// a test force-pushes. Statuses maps a commit hash to its JSON status array;
// a commit without an entry has none. LaterStatuses holds the arrays a
// commit's statuses change to, one per read, as checks finish.
type Fixture struct {
	State         string
	HeadCommit    string
	Statuses      map[string]string
	LaterStatuses map[string][]string
	Merge         MergeMode
	TaskPolls     int

	mu       sync.Mutex
	requests []Request
	polls    int
}

// Request is one request the fixture received.
type Request struct {
	Method string
	Path   string
	Body   string
}

func New() *Fixture {
	return &Fixture{State: "OPEN", HeadCommit: Head, Statuses: map[string]string{}, LaterStatuses: map[string][]string{}}
}

// Status is one commit status as Bitbucket returns it.
func Status(key, name, state string) string {
	return StatusAt(key, name, state, "https://ci.example/"+key)
}

// StatusAt is Status linking to url, such as a Pipelines result page.
func StatusAt(key, name, state, url string) string {
	return fmt.Sprintf(`{"type":"build","key":%q,"name":%q,"state":%q,"url":%q,"updated_on":"2026-11-10T12:00:00Z"}`, key, name, state, url)
}

func (f *Fixture) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Posted reports whether any POST reached the fixture.
func (f *Fixture) Posted() bool {
	for _, req := range f.Requests() {
		if req.Method == http.MethodPost {
			return true
		}
	}
	return false
}

func (f *Fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != Host {
		return nil, fmt.Errorf("mergetest: unexpected host %s", r.URL.Host)
	}
	recorder := httptest.NewRecorder()
	f.ServeHTTP(recorder, r)
	resp := recorder.Result()
	resp.Request = r
	return resp, nil
}

func (f *Fixture) pr(state string, merged bool) string {
	commit := ""
	if merged {
		commit = fmt.Sprintf(`,"merge_commit":{"hash":%q}`, MergeCommit)
	}
	return fmt.Sprintf(`{"id":7,"title":"Add login","state":%q,
		"source":{"branch":{"name":"feature/login"},"commit":{"hash":%q}},
		"destination":{"branch":{"name":"main"},"commit":{"hash":"0123456789ab"}},
		"links":{"html":{"href":%q}}%s}`, state, f.HeadCommit, URL, commit)
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == prPath:
		writeJSON(w, http.StatusOK, f.pr(f.State, f.State == "MERGED"))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, statusPath) && strings.HasSuffix(r.URL.Path, "/statuses"):
		commit := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, statusPath), "/statuses")
		f.mu.Lock()
		statuses := f.Statuses[commit]
		if later := f.LaterStatuses[commit]; len(later) > 0 {
			f.Statuses[commit], f.LaterStatuses[commit] = later[0], later[1:]
		}
		f.mu.Unlock()
		if statuses == "" {
			statuses = "[]"
		}
		writeJSON(w, http.StatusOK, `{"values":`+statuses+`}`)
	case r.Method == http.MethodPost && r.URL.Path == prPath+"/merge":
		f.merge(w, r)
	case r.Method == http.MethodGet && r.URL.Path == taskPath:
		f.mu.Lock()
		f.polls++
		done := f.Merge == MergeQueued && f.polls > f.TaskPolls
		f.mu.Unlock()
		if done {
			writeJSON(w, http.StatusOK, `{"task_status":"SUCCESS","merge_result":`+f.pr("MERGED", true)+`}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"task_status":"PENDING"}`)
	default:
		writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"not found"}}`)
	}
}

func (f *Fixture) merge(w http.ResponseWriter, r *http.Request) {
	switch f.Merge {
	case MergeNow:
		writeJSON(w, http.StatusOK, f.pr("MERGED", true))
	case MergeQueued, MergeStuck:
		scheme := r.URL.Scheme
		if scheme == "" {
			scheme = "http"
		}
		w.Header().Set("Location", scheme+"://"+r.Host+taskPath)
		w.WriteHeader(http.StatusAccepted)
	case MergeRefused:
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf(`{"type":"error","error":{"message":%q}}`, RefusalMessage))
	case MergeConflict:
		writeJSON(w, http.StatusConflict, `{"type":"error","error":{"message":"Merge conflicts in src/login.go"}}`)
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
