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

// The runner setup Bitbucket prints before a step's script, built from the
// shape of a real self-hosted db-backup run.
const (
	selfHostedRunner = "Runner matching labels:\n    - linux\n    - self.hosted\n" +
		"Runner name: devserver4_k8s_1\nRunner UUID: {abd695ff-509e-5a50-867d-6ed7f72097f3}\n" +
		"Runner labels: self.hosted, linux, devserver4\nRunner version:\n    current: 6.0.6\n    latest: 7.0.0\n\n\n" +
		"The version of this runner is outdated. Upgrade to the latest version (7.0.0). \n" +
		"Update instructions: https://support.atlassian.com/bitbucket-cloud/kb/updating-a-runner-version \n" +
		"Runner changelog: https://downloads.example.com/bitbucket/pipelines/CHANGELOG.md \n"
	cloneStart = "+ umask 000\n\n+ export GIT_LFS_SKIP_SMUDGE=1\n\n" +
		"+ retry 6 git clone --branch='testing' --depth 1 https://x-token-auth:$REPOSITORY_OAUTH_ACCESS_TOKEN@bitbucket.org/$BITBUCKET_REPO_FULL_NAME.git $BUILD_DIR\n" +
		"Cloning into '/opt/atlassian/pipelines/agent/build'...\n"
	cloneSetup = cloneStart + "\n+ git reset --hard 333ec33226f2aa11bb22cc33dd44ee55ff667788\nHEAD is now at 333ec33 fix: retry the dump on a timeout error\n\n" +
		"+ git config user.name bitbucket-pipelines\n\n+ git config user.email commits-noreply@bitbucket.org\n\n" +
		"+ git config push.default current\n\n+ git config http.${BITBUCKET_GIT_HTTP_ORIGIN}.proxy http://localhost:29418/\n\n" +
		"+ git remote set-url origin http://bitbucket.org/$BITBUCKET_REPO_FULL_NAME\n\n+ git reflog expire --expire=all --all\n\n" +
		"+ echo \".bitbucket/pipelines/generated\" >> .git/info/exclude\n\n+ chmod 777 $BUILD_DIR\n\n\n"
	variableNames = "Default variables:\n    BITBUCKET_BRANCH\n    BITBUCKET_BUILD_NUMBER\n    BITBUCKET_CLONE_DIR\n    BITBUCKET_COMMIT\n" +
		"    BITBUCKET_REPO_FULL_NAME\n    BITBUCKET_STEP_OIDC_TOKEN\n    BITBUCKET_WORKSPACE\n    CI\n    DOCKER_HOST\n\n" +
		"Workspace variables:\n    BITBUCKET_ACCESS_TOKEN\n    DOCKERHUB_PASSWORD\n    SONAR_TOKEN\n    VAULT_ROLE_ID\n    VAULT_SECRET_ID\n\n" +
		"Repository variables:\n    BITBUCKET_PASSWORD\n    BITBUCKET_USERNAME\n\n"
	selfHostedImage = "Runtime:\n    self-hosted:\n        storage: default\n\n" +
		"Images used:\n    build: us.icr.io/acme/pipeline-image@sha256:c6c369ed603fb782ba2eee5e8976a3703708ef18c92007b6bef2dc6c7716c3bd\n\n" +
		"Runner warnings:\n    us.icr.io/acme/pipeline-image:latest: Your kernel does not support memory swappiness capabilities or the cgroup is not mounted. Memory swappiness discarded.\n\n"
)

// BackupSetup is the runner setup at the start of build 2090's backup log.
const BackupSetup = selfHostedRunner + cloneSetup + variableNames +
	"Pipeline variables:\n    BRANCH: testing\n    META: iam\n\n" + selfHostedImage

// backupCommands are the script of the db-backup steps.
const backupCommands = `[{"commandType":"user","name":"cd pipelineImage","command":"cd pipelineImage"},
	{"commandType":"user","name":"chmod +x cicd-scripts/*.sh","command":"chmod +x cicd-scripts/*.sh"},
	{"commandType":"user","name":"./cicd-scripts/db_backup.sh","command":"./cicd-scripts/db_backup.sh"}]`

// FailedScript is what build 2090's failed "backup" step printed after its
// setup: the script's echo, then numbered lines, long enough that the
// default tail cuts it. FailedLog is the whole log.
var (
	FailedScript = func() string {
		var b strings.Builder
		b.WriteString("+ cd pipelineImage\n\n+ chmod +x cicd-scripts/*.sh\n\n+ ./cicd-scripts/db_backup.sh\n")
		for i := 1; b.Len() < 20000; i++ {
			fmt.Fprintf(&b, "backup line %05d\n", i)
		}
		return b.String()
	}()
	FailedLog = BackupSetup + FailedScript
)

// BackupImageShort is the build image BackupSetup names, with its digest
// cut as the setup line shows it.
const BackupImageShort = "us.icr.io/acme/pipeline-image@sha256:c6c369ed603f"

// Build 2091 failed in four steps. "clone" and "image" failed in setup, so
// their script never started. "pipe" ran a pipe whose first command the log
// echoes only after other commands, as a trace inside the script. "cloud" ran on a Bitbucket cloud runner and failed in its
// script, whose first command reads like a setup line; its run had secured,
// multi-line and more variables than Atlas shows. "long" passed with a log
// longer than the default read.
const (
	CloneFailure = "fatal: unable to access 'https://bitbucket.org/acme/widgets.git/': Could not resolve host: bitbucket.org\n"
	CloneLog     = selfHostedRunner + cloneStart + CloneFailure
	ImageFailure = "Failed to pull image us.icr.io/acme/missing:latest: manifest unknown\n"
	ImageLog     = selfHostedRunner + cloneSetup + variableNames +
		"Runtime:\n    self-hosted:\n        storage: default\n\nImages used:\n    build: us.icr.io/acme/missing:latest\n\n" + ImageFailure
	PipeLog = selfHostedRunner + cloneSetup + variableNames + selfHostedImage +
		"+ docker run atlassian/scp-deploy:1.5.0\nPulling pipe atlassian/scp-deploy:1.5.0\n+ pipe: atlassian/scp-deploy:1.5.0\nscp: connection refused\n"
	pipeCommands = `[{"commandType":"user","name":"pipe: atlassian/scp-deploy:1.5.0","command":"pipe: atlassian/scp-deploy:1.5.0"}]`

	CloudScript    = "+ umask 000\necho \"exporting $TARGET\"\nexporting staging\n\n+ exit 3\npg_dump: error: connection refused\n"
	cloudSetupHead = cloneSetup + "Cache \"pip\": Downloading\n" + CacheMiss + "\n\n" + variableNames + "Pipeline variables:\n"
	cloudSetupTail = "    TARGET: staging\n    NOTE: back up · then [verify]\n    MULTI: first line\n        second line\n    EMPTY:\n" +
		"    API_TOKEN: $API_TOKEN\n    DB_PASSWORD\n    SIGNING_KEY: ********\n    WRAPPED: ${WRAPPED}\n" +
		"%s\n" +
		"Images used:\n    build : docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc\n" +
		"    docker : docker-public.packages.atlassian.com/sox/atlassian/bitbucket-pipelines-docker-daemon@sha256:eb2a1b37ebc2\n\n" +
		"Runtime:\n    cloud:\n        version: 2\n        platform: linux\n        arch: x86\n\n"
	cloudCommands = `[{"commandType":"user","name":"umask 000","command":"umask 000\necho \"exporting $TARGET\"\n"},{"commandType":"user","name":"exit 3","command":"exit 3"}]`
)

// CacheMiss is a setup line of "cloud" that reads like a problem, so it is
// kept when the setup is cut.
const CacheMiss = `Cache "pip": Not found`

// MaskedForms are the masked secured values in "cloud"'s setup. None may
// reach output.
var MaskedForms = []string{"$API_TOKEN", "********", "${WRAPPED}"}

// LongKey and LongValue are "cloud"'s first variable, both longer than
// Atlas shows. The value's cap falls inside a two-byte rune.
var (
	LongKey   = strings.Repeat("K", 150)
	LongValue = "x" + strings.Repeat("é", 300)
)

// CloudSetup is the runner setup of 2091's "cloud" step: a long variable,
// eight that test parsing, and 25 more, so 34 in all.
var CloudSetup = func() string {
	var more strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&more, "    VAR_%02d: v%d\n", i, i)
	}
	return cloudSetupHead + "    " + LongKey + ": " + LongValue + "\n" + fmt.Sprintf(cloudSetupTail, strings.TrimSuffix(more.String(), "\n"))
}()

// LongScript is what 2091's passing "long" step printed after its setup,
// longer than a default read reaches.
var LongScript = func() string {
	var b strings.Builder
	b.WriteString("+ cd pipelineImage\n")
	for i := 1; b.Len() < 60000; i++ {
		fmt.Fprintf(&b, "long line %05d\n", i)
	}
	return b.String()
}()

// RunningLog is build 2096's running step log, with ANSI colour codes and a
// code fence in it.
const RunningLog = "\x1b[32mconnecting\x1b[0m\r\n```\nnot a fence\n```\nstill waiting\n"

// YAMLError is why Bitbucket failed build 2097 before any step ran.
const YAMLError = "There is a YAML syntax error in your bitbucket-pipelines.yml at [line 55, column 35]. Mapping values are not allowed here"

// Who started each run.
const (
	byToken = `"trigger":{"name":"MANUAL","type":"pipeline_trigger_manual"},
		"creator":{"type":"app_user","kind":"workspace_access_token","display_name":"cicd pipeline token","uuid":"{t-1}","account_id":"712020:t-1"}`
	byUser = `"trigger":{"name":"MANUAL","type":"pipeline_trigger_manual"},
		"creator":{"type":"user","display_name":"Merge Console","nickname":"merge","uuid":"{u-1}"}`
	byPush     = `"trigger":{"name":"PUSH","type":"pipeline_trigger_push"},"creator":{"type":"user","display_name":"Merge Console"}`
	bySchedule = `"trigger":{"name":"SCHEDULE","type":"pipeline_trigger_schedule"},"creator":null`
	unknown    = `"trigger":null,"creator":{}`
)

// The runs the fixture holds. 2090 failed in its backup step, 2091 in four,
// 2095 passed,
// 2096 is running, and Bitbucket failed 2097 itself on a YAML error. A
// workspace token started 2090 and 2097, a user 2095, a push 2096, and a
// schedule 2098; 2099 names no trigger or creator.
var pipelines = map[int]string{
	2090: pipelineJSON(2090, "COMPLETED", `"result":{"name":"FAILED"}`, "testing", "custom", "db-backup", 84, true, byToken),
	2091: pipelineJSON(2091, "COMPLETED", `"result":{"name":"FAILED"}`, "testing", "custom", "db-backup", 54, true, byToken),
	2095: pipelineJSON(2095, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "testing", "custom", "db-backup", 33, true, byUser),
	2096: pipelineJSON(2096, "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, "main", "branches", "main", 0, false, byPush),
	2097: pipelineJSON(2097, "COMPLETED", `"result":{"name":"FAILED","error":{"key":"plan-service.parse.detailed-parse-error","message":"`+YAMLError+`","type":"pipeline_error"}}`, "fix/yaml", "custom", "db-backup", 0, true, byToken),
	2098: pipelineJSON(2098, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "main", "custom", "nightly", 5, true, bySchedule),
	2099: pipelineJSON(2099, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "main", "branches", "main", 5, true, unknown),
}

var (
	failed    = `"result":{"name":"FAILED"}`
	succeeded = `"result":{"name":"SUCCESSFUL"}`
)

var steps = map[int]string{
	2090: `[` + stepJSON("s-build", "build", "COMPLETED", succeeded, 12, true, "") + `,` +
		stepJSON("s-backup", "backup", "COMPLETED", failed, 72, true, backupCommands) + `,` +
		stepJSON("s-deploy", "deploy", "COMPLETED", `"result":{"name":"NOT_RUN"}`, 0, false, backupCommands) + `]`,
	2091: `[` + stepJSON("s-clone", "clone", "COMPLETED", failed, 5, true, backupCommands) + `,` +
		stepJSON("s-image", "image", "COMPLETED", failed, 9, true, backupCommands) + `,` +
		stepJSON("s-pipe", "pipe", "COMPLETED", failed, 14, true, pipeCommands) + `,` +
		stepJSON("s-cloud", "cloud", "COMPLETED", failed, 12, true, cloudCommands) + `,` +
		stepJSON("s-long", "long", "COMPLETED", succeeded, 40, true, backupCommands) + `]`,
	2095: `[` + stepJSON("s-ok", "backup", "COMPLETED", succeeded, 33, true, backupCommands) + `]`,
	2096: `[` + stepJSON("s-run", "backup", "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, 0, true, backupCommands) + `]`,
	2097: `[]`,
	2098: `[]`,
	2099: `[]`,
}

var logs = map[string]string{
	"2090/s-build":  "build ok\n",
	"2090/s-backup": FailedLog,
	"2091/s-clone":  CloneLog,
	"2091/s-image":  ImageLog,
	"2091/s-pipe":   PipeLog,
	"2091/s-cloud":  CloudSetup + CloudScript,
	"2091/s-long":   BackupSetup + LongScript,
	"2095/s-ok":     "backup ok\n",
	"2096/s-run":    RunningLog,
}

func pipelineJSON(n int, state, detail, branch, selectorType, pattern string, seconds int, done bool, who string) string {
	completed := ""
	if done {
		completed = `"completed_on":"2026-11-02T18:47:01Z",`
	}
	return fmt.Sprintf(`{"uuid":"{p-%d}","build_number":%d,"state":{"name":%q,%s},
		"target":{"type":"pipeline_ref_target","ref_type":"branch","ref_name":%q,"commit":{"hash":"333ec33226f2aa11bb22cc33dd44ee55ff667788"},"selector":{"type":%q,"pattern":%q}},
		%s,
		"created_on":"2026-11-02T18:45:37Z",%s"duration_in_seconds":%d}`,
		n, n, state, detail, branch, selectorType, pattern, who, completed, seconds)
}

func stepJSON(uuid, name, state, detail string, seconds int, started bool, commands string) string {
	times := ""
	if started {
		times = `"started_on":"2026-11-02T18:45:38Z",`
		if state == "COMPLETED" {
			times += `"completed_on":"2026-11-02T18:47:00Z",`
		}
	}
	script := ""
	if commands != "" {
		script = `,"script_commands":` + commands
	}
	return fmt.Sprintf(`{"uuid":"{%s}","name":%q,"state":{"name":%q,%s},%s"duration_in_seconds":%d%s}`, uuid, name, state, detail, times, seconds, script)
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
		_, _ = io.WriteString(w, pipelineJSON(Started, "PENDING", `"stage":{"name":"PENDING"}`, "main", "custom", "db-backup", 0, false, byUser))
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
	for _, n := range []int{2097, 2096, 2095, 2090} {
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
			writeJSON(w, pipelineJSON(Started, "COMPLETED", `"result":{"name":"SUCCESSFUL"}`, "main", "custom", "db-backup", 41, true, byUser))
			return
		}
		writeJSON(w, pipelineJSON(Started, "IN_PROGRESS", `"stage":{"name":"RUNNING"}`, "main", "custom", "db-backup", 0, false, byUser))
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
