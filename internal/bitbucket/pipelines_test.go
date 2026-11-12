package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const testPipelineUUID = "{11111111-2222-3333-4444-555555555555}"

const testPipelineJSON = `{
	"uuid":"{11111111-2222-3333-4444-555555555555}",
	"build_number":42,
	"state":{"name":"COMPLETED","type":"pipeline_state_completed","result":{"name":"FAILED","type":"pipeline_state_completed_failed"}},
	"target":{"type":"pipeline_ref_target","ref_type":"branch","ref_name":"main","commit":{"hash":"abc123"},"selector":{"type":"custom","pattern":"db-backup"}},
	"trigger":{"name":"MANUAL","type":"pipeline_trigger_manual"},
	"creator":{"display_name":"Jane","uuid":"{jane}"},
	"created_on":"2026-01-02T03:04:05Z",
	"completed_on":"2026-01-02T03:14:05Z",
	"duration_in_seconds":600
}`

func TestListPipelinesSendsFilters(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, `{"page":1,"pagelen":10,"size":500,"values":[`+testPipelineJSON+`]}`)
	})

	pipelines, err := client.ListPipelines(context.Background(), "ws", "repo", &PipelineListOptions{
		Branch:          "main",
		Commit:          "abc123",
		SelectorPattern: "db-backup",
		SelectorType:    "custom",
		PageLen:         10,
	})
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 1 {
		t.Fatalf("len(pipelines) = %d, want one page only", len(pipelines))
	}

	p := pipelines[0]
	if p.BuildNumber != 42 || p.UUID != testPipelineUUID || p.State.Status() != "FAILED" || p.State.Name != "COMPLETED" {
		t.Fatalf("pipeline = %#v", p)
	}
	if p.Target.Branch() != "main" || p.Target.CommitHash() != "abc123" || p.Target.Selector.Pattern != "db-backup" {
		t.Fatalf("target = %#v", p.Target)
	}
	if p.Trigger.Name != "MANUAL" || p.Creator.DisplayName != "Jane" || p.CompletedOn == nil || p.DurationInSeconds != 600 {
		t.Fatalf("pipeline metadata = %#v", p)
	}

	requests := recorder.all()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	req := requests[0]
	expectRequest(t, req, http.MethodGet, "/repositories/ws/repo/pipelines/")
	want := url.Values{
		"target.branch":           {"main"},
		"target.commit.hash":      {"abc123"},
		"target.selector.pattern": {"db-backup"},
		"target.selector.type":    {"custom"},
		"sort":                    {"-created_on"},
		"pagelen":                 {"10"},
	}
	if req.Query.Encode() != want.Encode() {
		t.Fatalf("query = %s, want %s", req.Query.Encode(), want.Encode())
	}
}

func TestListPipelinesWithLimitFollowsNextLinks(t *testing.T) {
	t.Parallel()

	var serverURL string
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			next := serverURL + "/repositories/ws/repo/pipelines/?page=2&pagelen=2&sort=created_on"
			writeJSON(w, http.StatusOK, `{"page":1,"pagelen":2,"size":5,"next":"`+next+`","values":[{"build_number":5},{"build_number":4}]}`)
		case "2":
			writeJSON(w, http.StatusOK, `{"page":2,"pagelen":2,"size":5,"values":[{"build_number":3},{"build_number":2}]}`)
		default:
			t.Errorf("unexpected page %s", page)
			writeJSON(w, http.StatusOK, `{"values":[]}`)
		}
	})
	serverURL = server.URL

	pipelines, err := client.ListPipelines(context.Background(), "ws", "repo", &PipelineListOptions{Limit: 3, PageLen: 2, Sort: "created_on"})
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 3 || pipelines[2].BuildNumber != 3 {
		t.Fatalf("pipelines = %#v, want builds 5,4,3", pipelines)
	}
	requests := recorder.all()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].Query.Get("sort") != "created_on" || requests[1].Query.Get("page") != "2" {
		t.Fatalf("queries = %v / %v", requests[0].Query, requests[1].Query)
	}
}

func TestGetPipelineByBuildNumberAndUUID(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusOK, testPipelineJSON)
	})

	for _, ref := range []string{"42", "#42"} {
		if _, err := client.GetPipeline(context.Background(), "ws", "repo", ref); err != nil {
			t.Fatalf("GetPipeline(%q) error = %v", ref, err)
		}
	}
	if _, err := client.GetPipeline(context.Background(), "ws", "repo", strings.Trim(testPipelineUUID, "{}")); err != nil {
		t.Fatalf("GetPipeline(uuid) error = %v", err)
	}

	requests := recorder.all()
	for _, req := range requests[:2] {
		expectRequest(t, req, http.MethodGet, "/repositories/ws/repo/pipelines/42")
	}
	if requests[2].Path != "/repositories/ws/repo/pipelines/"+testPipelineUUID {
		t.Fatalf("uuid path = %s", requests[2].Path)
	}
	if !strings.Contains(requests[2].EscapedPath, "%7B11111111") {
		t.Fatalf("escaped path = %s, want encoded braces", requests[2].EscapedPath)
	}
}

func TestGetPipelineNotFound(t *testing.T) {
	t.Parallel()

	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"Not found"}}`)
	})

	_, err := client.GetPipeline(context.Background(), "ws", "repo", "99")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "pipeline '99'") {
		t.Fatalf("error = %v, want pipeline not found", err)
	}
}

func TestPipelineStepsResolveBuildNumberToUUID(t *testing.T) {
	t.Parallel()

	stepsPath := "/repositories/ws/repo/pipelines/" + testPipelineUUID + "/steps"
	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/repositories/ws/repo/pipelines/42":
			writeJSON(w, http.StatusOK, testPipelineJSON)
		case stepsPath:
			writeJSON(w, http.StatusOK, `{"page":1,"pagelen":100,"size":1,"values":[{"uuid":"{step-1}","name":"Build","state":{"name":"COMPLETED","result":{"name":"SUCCESSFUL"}},"started_on":"2026-01-02T03:04:05Z","completed_on":"2026-01-02T03:05:05Z","duration_in_seconds":60}]}`)
		case stepsPath + "/{step-1}":
			writeJSON(w, http.StatusOK, `{"uuid":"{step-1}","name":"Build","state":{"name":"IN_PROGRESS"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	})

	steps, err := client.ListPipelineSteps(context.Background(), "ws", "repo", "42")
	if err != nil {
		t.Fatalf("ListPipelineSteps() error = %v", err)
	}
	if len(steps) != 1 || steps[0].Name != "Build" || steps[0].State.Status() != "SUCCESSFUL" || steps[0].StartedOn == nil || steps[0].CompletedOn == nil || steps[0].DurationInSeconds != 60 {
		t.Fatalf("steps = %#v", steps)
	}

	step, err := client.GetPipelineStep(context.Background(), "ws", "repo", testPipelineUUID, "step-1")
	if err != nil {
		t.Fatalf("GetPipelineStep() error = %v", err)
	}
	if step.State.Status() != "IN_PROGRESS" || step.StartedOn != nil {
		t.Fatalf("step = %#v", step)
	}

	requests := recorder.all()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want build lookup, steps, step", len(requests))
	}
	expectRequest(t, requests[0], http.MethodGet, "/repositories/ws/repo/pipelines/42")
	expectRequest(t, requests[1], http.MethodGet, stepsPath)
	if requests[1].Query.Get("pagelen") != "100" {
		t.Fatalf("steps query = %v", requests[1].Query)
	}
}

type headerInjectingTransport struct {
	base http.RoundTripper
}

func (t headerInjectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Atlas-Trace", "trace-1")
	clone.Header.Set("X-Atlas-Session", "session-1")
	return t.base.RoundTrip(clone)
}

func TestGetPipelineStepLogFollowsRedirectWithoutCredentials(t *testing.T) {
	t.Parallel()

	var storageHits atomic.Int32
	var storageHeaders http.Header
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageHits.Add(1)
		storageHeaders = r.Header.Clone()
		if r.URL.Query().Get("X-Amz-Signature") != "sig" {
			t.Errorf("storage query = %v, want presigned params", r.URL.Query())
		}
		w.Header().Set("Content-Range", "bytes 990-999/1000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "end of log")
	}))
	t.Cleanup(storage.Close)

	var apiHits atomic.Int32
	var apiHeaders http.Header
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits.Add(1)
		apiHeaders = r.Header.Clone()
		wantPath := "/repositories/ws/repo/pipelines/" + testPipelineUUID + "/steps/{step-1}/log"
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "secret"})
		w.Header().Set("Location", storage.URL+"/logs/step-1?X-Amz-Signature=sig")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(api.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	storageURL, _ := url.Parse(storage.URL)
	jar.SetCookies(storageURL, []*http.Cookie{{Name: "storage-session", Value: "leak"}})
	authenticated := &http.Client{
		Jar:       jar,
		Transport: headerInjectingTransport{base: http.DefaultTransport},
	}
	client := newClientForTest(api.URL, authenticated)

	log, err := client.GetPipelineStepLog(context.Background(), "ws", "repo", testPipelineUUID, "{step-1}", 10)
	if err != nil {
		t.Fatalf("GetPipelineStepLog() error = %v", err)
	}
	if string(log.Content) != "end of log" || !log.Partial || log.TotalSize != 1000 || log.ContentRange != "bytes 990-999/1000" {
		t.Fatalf("log = %#v", log)
	}

	if apiHits.Load() != 1 || storageHits.Load() != 1 {
		t.Fatalf("api hits = %d, storage hits = %d, want 1 each", apiHits.Load(), storageHits.Load())
	}
	if !strings.HasPrefix(apiHeaders.Get("Authorization"), "Basic ") || apiHeaders.Get("Range") != "bytes=-10" {
		t.Fatalf("api headers = %v, want credentials and range", apiHeaders)
	}
	if storageHeaders.Get("Authorization") != "" {
		t.Fatalf("storage received Authorization %q", storageHeaders.Get("Authorization"))
	}
	if storageHeaders.Get("Cookie") != "" {
		t.Fatalf("storage received Cookie %q", storageHeaders.Get("Cookie"))
	}
	for name := range storageHeaders {
		if strings.HasPrefix(strings.ToLower(name), "x-atlas") {
			t.Fatalf("storage received %s header", name)
		}
	}
	if storageHeaders.Get("Range") != "bytes=-10" {
		t.Fatalf("storage Range = %q, want forwarded", storageHeaders.Get("Range"))
	}
}

func TestGetPipelineStepLogDirectAndEmpty(t *testing.T) {
	t.Parallel()

	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case strings.Contains(r.URL.Path, "/steps/{empty}/log"):
			w.Header().Set("Content-Range", "bytes */0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		case strings.Contains(r.URL.Path, "/steps/{missing}/log"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "full log")
		}
	})

	full, err := client.GetPipelineStepLog(context.Background(), "ws", "repo", testPipelineUUID, "full", 64)
	if err != nil {
		t.Fatalf("full log error = %v", err)
	}
	if string(full.Content) != "full log" || full.Partial || full.TotalSize != 8 {
		t.Fatalf("full log = %#v", full)
	}

	empty, err := client.GetPipelineStepLog(context.Background(), "ws", "repo", testPipelineUUID, "empty", 10)
	if err != nil {
		t.Fatalf("empty log error = %v", err)
	}
	if len(empty.Content) != 0 || empty.TotalSize != 0 {
		t.Fatalf("empty log = %#v", empty)
	}

	_, err = client.GetPipelineStepLog(context.Background(), "ws", "repo", testPipelineUUID, "missing", 10)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing log error = %v, want not found", err)
	}
}

func TestRunPipelineBuildsTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input PipelineRunInput
		want  string
	}{
		{
			name: "branch with custom pipeline and variables",
			input: PipelineRunInput{
				Branch:         "main",
				CustomPipeline: "db-backup",
				Variables:      []PipelineVariable{{Key: "ENV", Value: "prod"}, {Key: "TOKEN", Value: "s3cret", Secured: true}},
			},
			want: `{"target":{"type":"pipeline_ref_target","ref_type":"branch","ref_name":"main","selector":{"type":"custom","pattern":"db-backup"}},"variables":[{"key":"ENV","value":"prod","secured":false},{"key":"TOKEN","value":"s3cret","secured":true}]}`,
		},
		{
			name:  "commit only",
			input: PipelineRunInput{Commit: "abc123"},
			want:  `{"target":{"type":"pipeline_commit_target","commit":{"type":"commit","hash":"abc123"}}}`,
		},
		{
			name:  "branch pinned to commit",
			input: PipelineRunInput{Branch: "main", Commit: "abc123"},
			want:  `{"target":{"type":"pipeline_ref_target","ref_type":"branch","ref_name":"main","commit":{"type":"commit","hash":"abc123"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusCreated, `{"uuid":"{new}","build_number":43,"state":{"name":"PENDING"}}`)
			})

			pipeline, err := client.RunPipeline(context.Background(), "ws", "repo", tt.input)
			if err != nil {
				t.Fatalf("RunPipeline() error = %v", err)
			}
			if pipeline.BuildNumber != 43 {
				t.Fatalf("pipeline = %#v", pipeline)
			}
			req := recorder.all()[0]
			expectRequest(t, req, http.MethodPost, "/repositories/ws/repo/pipelines/")
			if req.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("Content-Type = %q", req.Header.Get("Content-Type"))
			}
			if mustJSON(t, decodeBody(t, req.Body)) != mustJSON(t, decodeBody(t, []byte(tt.want))) {
				t.Fatalf("body = %s, want %s", req.Body, tt.want)
			}
		})
	}
}

func TestRunPipelineValidationAndErrors(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, http.StatusBadRequest, `{"type":"error","error":{"message":"Not found","detail":"Could not find a custom pipeline named 'nope'"}}`)
	})

	if _, err := client.RunPipeline(context.Background(), "ws", "repo", PipelineRunInput{}); err == nil {
		t.Fatal("RunPipeline(empty) error = nil, want error")
	}
	if len(recorder.all()) != 0 {
		t.Fatal("request sent without a target")
	}

	_, err := client.RunPipeline(context.Background(), "ws", "repo", PipelineRunInput{Branch: "main", CustomPipeline: "nope"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Not found: Could not find a custom pipeline named 'nope'" {
		t.Fatalf("error = %v, want Bitbucket message and detail", err)
	}
}

func TestStopPipelineByBuildNumber(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, testPipelineJSON)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.StopPipeline(context.Background(), "ws", "repo", "42"); err != nil {
		t.Fatalf("StopPipeline() error = %v", err)
	}
	requests := recorder.all()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want lookup and stop", len(requests))
	}
	expectRequest(t, requests[0], http.MethodGet, "/repositories/ws/repo/pipelines/42")
	expectRequest(t, requests[1], http.MethodPost, "/repositories/ws/repo/pipelines/"+testPipelineUUID+"/stopPipeline")
}

func TestPipelineWebURL(t *testing.T) {
	t.Parallel()

	if got := PipelineWebURL("ws", "repo", 42); got != "https://bitbucket.org/ws/repo/pipelines/results/42" {
		t.Fatalf("PipelineWebURL() = %q", got)
	}
}

func TestGetPipelineStepLogKeepsTheTailWhenTheRangeIsIgnored(t *testing.T) {
	t.Parallel()

	client, _, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, strings.Repeat("a", 100<<10)+"the tail")
	})
	log, err := client.GetPipelineStepLog(context.Background(), "ws", "repo", testPipelineUUID, "full", 8)
	if err != nil {
		t.Fatal(err)
	}
	if string(log.Content) != "the tail" || log.TotalSize != 100<<10+8 {
		t.Fatalf("log = %q of %d bytes", log.Content, log.TotalSize)
	}
}
