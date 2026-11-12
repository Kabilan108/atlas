// Package prtest serves one fixed Bitbucket pull request so tests of the CLI,
// the ops layer, and the MCP server can compare their output on the same data.
package prtest

import (
	"embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const (
	Workspace = "acme"
	Repo      = "widgets"
	PRID      = 42
	Branch    = "feature/login"
	URL       = "https://bitbucket.org/acme/widgets/pull-requests/42"

	// Host is the only host RoundTrip serves.
	Host = "api.bitbucket.org"
)

//go:embed testdata
var testdata embed.FS

// Golden returns a file from testdata, recorded from `atlas pr view` before
// the ops layer existed.
func Golden(name string) string {
	data, err := testdata.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// GoldenPath is where Golden reads name from, relative to this package.
func GoldenPath(name string) string {
	return "testdata/" + name
}

// Request is one request the fixture received.
type Request struct {
	Method   string
	Path     string
	Username string
	Password string
	Body     string
}

// Fixture is a fake Bitbucket API holding pull request #42 of acme/widgets.
type Fixture struct {
	mu       sync.Mutex
	requests []Request
	writes   writeState
}

func New() *Fixture {
	return &Fixture{}
}

// Requests returns the requests served so far.
func (f *Fixture) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// RoundTrip serves requests for api.bitbucket.org in-process, so a client with
// the default base URL talks to the fixture.
func (f *Fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != Host {
		return nil, fmt.Errorf("prtest: unexpected host %s", r.URL.Host)
	}
	recorder := httptest.NewRecorder()
	f.ServeHTTP(recorder, r)
	resp := recorder.Result()
	resp.Request = r
	return resp, nil
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, password := basicAuth(r)
	body := readBody(r)
	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.RequestURI(), Username: user, Password: password, Body: body})
	f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/2.0")
	if f.serveMore(w, r, path, body) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "read-only fixture")
		return
	}
	prPath := fmt.Sprintf("/repositories/%s/%s/pullrequests", Workspace, Repo)
	switch {
	case path == fmt.Sprintf("%s/%d", prPath, PRID):
		writeJSON(w, pullRequestJSON)
	case path == prPath && r.URL.Query().Get("q") == fmt.Sprintf("source.branch.name=%q", Branch):
		writeJSON(w, `{"values":[`+pullRequestJSON+`]}`)
	case path == fmt.Sprintf("%s/%d/comments", prPath, PRID):
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, `{"values":[`+commentsPage2+`]}`)
			return
		}
		next := apiBase(r) + fmt.Sprintf("%s/%d/comments?page=2", prPath, PRID)
		writeJSON(w, `{"next":"`+next+`","values":[`+commentsPage1+`]}`)
	case path == fmt.Sprintf("%s/%d/tasks", prPath, PRID):
		writeJSON(w, `{"values":[`+tasksJSON+`]}`)
	case path == fmt.Sprintf("%s/%d/diff", prPath, PRID):
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, diffText)
	case path == "/users/557058:mentioned":
		writeJSON(w, mentionedUserJSON)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func basicAuth(r *http.Request) (string, string) {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Basic ")
	if !ok {
		return "", ""
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", ""
	}
	user, password, _ := strings.Cut(string(decoded), ":")
	return user, password
}

func apiBase(r *http.Request) string {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host + "/2.0"
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"type":"error","error":{"message":%q}}`, message)
}

const (
	ada   = `{"display_name":"Ada Lovelace","nickname":"ada","account_id":"557058:ada","uuid":"{ada-uuid}"}`
	grace = `{"display_name":"Grace Hopper","nickname":"grace","account_id":"557058:grace","uuid":"{grace-uuid}"}`
	linus = `{"display_name":"Linus Torvalds","nickname":"linus","account_id":"557058:linus","uuid":"{linus-uuid}"}`
	ken   = `{"display_name":"Ken Thompson","nickname":"ken","account_id":"557058:ken","uuid":"{ken-uuid}"}`
)

const mentionedUserJSON = `{"display_name":"Margaret Hamilton","nickname":"margaret","account_id":"557058:mentioned","uuid":"{margaret-uuid}"}`

const pullRequestJSON = `{
  "id": 42,
  "title": "Fix login: handle 'empty' users",
  "description": "Fixes login for @{557058:mentioned} and @{557058:grace}.\n\n- returns errEmpty\n- keeps the session\n\n` + "```" + `\n@{557058:in-fence}\n` + "```" + `\n\nSee ` + "`@{557058:inline-code}`" + ` for details.",
  "state": "OPEN",
  "draft": false,
  "author": ` + ada + `,
  "source": {"branch": {"name": "feature/login"}, "commit": {"hash": "abc1234"}, "repository": {"full_name": "acme/widgets"}},
  "destination": {"branch": {"name": "main"}, "commit": {"hash": "def5678"}, "repository": {"full_name": "acme/widgets"}},
  "reviewers": [` + grace + `, ` + linus + `, ` + ken + `],
  "participants": [
    {"user": ` + grace + `, "role": "REVIEWER", "approved": true, "state": "approved"},
    {"user": ` + linus + `, "role": "REVIEWER", "approved": false, "state": "changes_requested"},
    {"user": ` + ada + `, "role": "PARTICIPANT", "approved": false, "state": null}
  ],
  "created_on": "2025-03-01T09:00:00.000000+00:00",
  "updated_on": "2025-03-05T16:30:00.000000+00:00",
  "links": {"html": {"href": "https://bitbucket.org/acme/widgets/pull-requests/42"}},
  "comment_count": 5,
  "task_count": 2
}`

const commentsPage1 = `{
  "id": 101,
  "content": {"raw": "Handle the error here. Also loop in @{557058:mentioned}.", "markup": "markdown", "html": "<p>Handle the error here. Also loop in <a href=\"https://bitbucket.org/margaret\">@Margaret Hamilton</a>.</p>"},
  "user": ` + grace + `,
  "created_on": "2025-03-02T10:00:00.000000+00:00",
  "updated_on": "2025-03-02T10:00:00.000000+00:00",
  "inline": {"path": "src/login.go", "to": 12},
  "deleted": false
},
{
  "id": 102,
  "content": {"raw": "Done, thanks.", "markup": "markdown", "html": ""},
  "user": ` + ada + `,
  "created_on": "2025-03-02T11:00:00.000000+00:00",
  "updated_on": "2025-03-02T11:00:00.000000+00:00",
  "inline": {"path": "src/login.go", "to": 12},
  "parent": {"id": 101},
  "deleted": false
}`

const commentsPage2 = `{
  "id": 103,
  "content": {"raw": "Please add tests for @{557058:ghost} before merging. Otherwise fine.", "markup": "markdown", "html": ""},
  "user": ` + linus + `,
  "created_on": "2025-03-03T08:00:00.000000+00:00",
  "updated_on": "2025-03-03T08:00:00.000000+00:00",
  "deleted": false
},
{
  "id": 104,
  "content": {"raw": "Typo in the heading.", "markup": "markdown", "html": ""},
  "user": ` + grace + `,
  "created_on": "2025-03-03T09:00:00.000000+00:00",
  "updated_on": "2025-03-03T09:00:00.000000+00:00",
  "inline": {"path": "README.md"},
  "resolution": {"user": ` + ada + `, "date": "2025-03-04T09:00:00.000000+00:00"},
  "deleted": false
},
{
  "id": 105,
  "content": {"raw": "", "markup": "markdown", "html": ""},
  "user": ` + ken + `,
  "created_on": "2025-03-03T10:00:00.000000+00:00",
  "updated_on": "2025-03-03T10:00:00.000000+00:00",
  "deleted": true
}`

const tasksJSON = `{
  "id": 1,
  "content": {"raw": "Add tests for the empty user", "markup": "markdown", "html": ""},
  "state": "UNRESOLVED",
  "comment": {"id": 101, "user": ` + linus + `, "inline": {"path": "src/login.go", "to": 12}}
},
{
  "id": 2,
  "content": {"raw": "Fix typo flagged by @{557058:mentioned}", "markup": "markdown", "html": ""},
  "state": "RESOLVED",
  "comment": {"id": 104, "user": ` + grace + `}
}`

const diffText = `diff --git a/src/login.go b/src/login.go
index 1111111..2222222 100644
--- a/src/login.go
+++ b/src/login.go
@@ -8,5 +8,7 @@ func Login(user string) error {
 	if user == "" {
 		return errEmpty
 	}
+	token, err := issue(user)
+	if err != nil { return err }
 	return save(token)
 }
`
