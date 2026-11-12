// Package pipelinetest serves a fake Bitbucket Pipelines API for acme/widgets,
// so op tests and CLI tests read the same runs.
package pipelinetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

const (
	Workspace = "acme"
	Repo      = "widgets"
	// Host is the only host RoundTrip serves.
	Host = "api.bitbucket.org"

	// Started is the build number a POST to pipelines/ creates.
	Started = 2100
)

// FailedLog is the log of build 2090's failed "backup" step: numbered lines,
// long enough that the default tail cuts it.
var FailedLog = func() string {
	var b strings.Builder
	for i := 1; b.Len() < 20000; i++ {
		fmt.Fprintf(&b, "backup line %05d\n", i)
	}
	return b.String()
}()

// RunningLog is build 2096's running step log, with ANSI colour codes and a
// code fence in it.
const RunningLog = "\x1b[32mconnecting\x1b[0m\r\n```\nnot a fence\n```\nstill waiting\n"

// The runs the fixture holds. 2090 failed in its backup step, 2095 passed,
// 2096 is running.
var pipelines = map[int]string{
	2090: pipelineJSON(2090, "COMPLETED", `"result":{"name":"FAILED"}`, "testing", "custom", "db-backup", 84, true),
	2095: pipelineJSON(2095, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "testing", "custom", "db-backup", 33, true),
	2096: pipelineJSON(2096, "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, "main", "branches", "main", 0, false),
}

var steps = map[int]string{
	2090: `[` + stepJSON("s-build", "build", "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, 12, true) + `,` +
		stepJSON("s-backup", "backup", "COMPLETED", `"result":{"name":"FAILED"}`, 72, true) + `,` +
		stepJSON("s-deploy", "deploy", "COMPLETED", `"result":{"name":"NOT_RUN"}`, 0, false) + `]`,
	2095: `[` + stepJSON("s-ok", "backup", "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, 33, true) + `]`,
	2096: `[` + stepJSON("s-run", "backup", "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, 0, true) + `]`,
}

var logs = map[string]string{
	"2090/s-build":  "build ok\n",
	"2090/s-backup": FailedLog,
	"2095/s-ok":     "backup ok\n",
	"2096/s-run":    RunningLog,
}

func pipelineJSON(n int, state, detail, branch, selectorType, pattern string, seconds int, done bool) string {
	completed := ""
	if done {
		completed = `"completed_on":"2026-11-02T18:47:01Z",`
	}
	return fmt.Sprintf(`{"uuid":"{p-%d}","build_number":%d,"state":{"name":%q,%s},
		"target":{"type":"pipeline_ref_target","ref_type":"branch","ref_name":%q,"commit":{"hash":"333ec33226f2aa11bb22cc33dd44ee55ff667788"},"selector":{"type":%q,"pattern":%q}},
		"trigger":{"name":"MANUAL","type":"pipeline_trigger_manual"},"creator":{"display_name":"Merge Console"},
		"created_on":"2026-11-02T18:45:37Z",%s"duration_in_seconds":%d}`,
		n, n, state, detail, branch, selectorType, pattern, completed, seconds)
}

func stepJSON(uuid, name, state, detail string, seconds int, started bool) string {
	times := ""
	if started {
		times = `"started_on":"2026-11-02T18:45:38Z",`
		if state == "COMPLETED" {
			times += `"completed_on":"2026-11-02T18:47:00Z",`
		}
	}
	return fmt.Sprintf(`{"uuid":"{%s}","name":%q,"state":{"name":%q,%s},%s"duration_in_seconds":%d}`, uuid, name, state, detail, times, seconds)
}

// Request is one request the fixture received.
type Request struct {
	Method string
	Path   string
	Range  string
	Body   string
}

// Fixture is the fake API. StartedPolls is how many GETs of the started run
// (build 2100) answer IN_PROGRESS before it completes; a negative value
// keeps it running forever.
type Fixture struct {
	StartedPolls int

	mu       sync.Mutex
	requests []Request
	polls    int
}

func New() *Fixture {
	return &Fixture{StartedPolls: -1}
}

// Requests returns the requests served so far.
func (f *Fixture) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Count returns how many requests had method and path.
func (f *Fixture) Count(method, path string) int {
	n := 0
	for _, req := range f.Requests() {
		if req.Method == method && req.Path == path {
			n++
		}
	}
	return n
}

// RoundTrip serves requests for api.bitbucket.org in-process.
func (f *Fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != Host {
		return nil, fmt.Errorf("pipelinetest: unexpected host %s", r.URL.Host)
	}
	recorder := httptest.NewRecorder()
	f.ServeHTTP(recorder, r)
	resp := recorder.Result()
	resp.Request = r
	return resp, nil
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.RequestURI(), Range: r.Header.Get("Range"), Body: string(body)})
	f.mu.Unlock()

	base := fmt.Sprintf("/2.0/repositories/%s/%s/pipelines/", Workspace, Repo)
	rest, ok := strings.CutPrefix(r.URL.Path, base)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		f.list(w, r)
	case rest == "" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, pipelineJSON(Started, "PENDING", `"stage":{"name":"PENDING"}`, "main", "custom", "db-backup", 0, false))
	case len(parts) == 1 && r.Method == http.MethodGet:
		f.pipeline(w, parts[0])
	case len(parts) == 2 && parts[1] == "steps":
		f.steps(w, parts[0])
	case len(parts) == 4 && parts[1] == "steps" && parts[3] == "log":
		f.log(w, r, parts[0], parts[2])
	case len(parts) == 2 && parts[1] == "stopPipeline" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (f *Fixture) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var values []string
	for _, n := range []int{2096, 2095, 2090} {
		var run struct {
			Target struct {
				RefName  string `json:"ref_name"`
				Selector struct {
					Pattern string `json:"pattern"`
				} `json:"selector"`
			} `json:"target"`
		}
		_ = json.Unmarshal([]byte(pipelines[n]), &run)
		if branch := query.Get("target.branch"); branch != "" && branch != run.Target.RefName {
			continue
		}
		if pattern := query.Get("target.selector.pattern"); pattern != "" && pattern != run.Target.Selector.Pattern {
			continue
		}
		values = append(values, pipelines[n])
	}
	writeJSON(w, `{"values":[`+strings.Join(values, ",")+`]}`)
}

// buildNumber maps a path segment (a build number or a braced UUID) to a
// build number, or 0.
func buildNumber(segment string) int {
	if n, err := strconv.Atoi(segment); err == nil {
		return n
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(segment, "{p-"), "}"))
	return n
}

func (f *Fixture) pipeline(w http.ResponseWriter, segment string) {
	n := buildNumber(segment)
	if n == Started {
		f.mu.Lock()
		f.polls++
		done := f.StartedPolls >= 0 && f.polls > f.StartedPolls
		f.mu.Unlock()
		if done {
			writeJSON(w, pipelineJSON(Started, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "main", "custom", "db-backup", 41, true))
			return
		}
		writeJSON(w, pipelineJSON(Started, "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, "main", "custom", "db-backup", 0, false))
		return
	}
	run, ok := pipelines[n]
	if !ok {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	writeJSON(w, run)
}

func (f *Fixture) steps(w http.ResponseWriter, segment string) {
	list, ok := steps[buildNumber(segment)]
	if !ok {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	writeJSON(w, `{"values":`+list+`}`)
}

// log serves a step log, honouring suffix ranges (bytes=-N) as Bitbucket
// does.
func (f *Fixture) log(w http.ResponseWriter, r *http.Request, pipeline, step string) {
	key := fmt.Sprintf("%d/%s", buildNumber(pipeline), strings.Trim(step, "{}"))
	content, ok := logs[key]
	if !ok {
		writeError(w, http.StatusNotFound, "log not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	suffix, ranged := strings.CutPrefix(r.Header.Get("Range"), "bytes=-")
	if !ranged {
		_, _ = io.WriteString(w, content)
		return
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n <= 0 {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start := max(0, len(content)-n)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = io.WriteString(w, content[start:])
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"error","error":{"message":%q}}`, message)
}
