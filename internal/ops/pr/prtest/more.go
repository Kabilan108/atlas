package prtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Ada is the author of #42 and Grace the user the fixture's credentials
// belong to (GET /user): she reviews #42 and wrote #43.
const (
	AdaID   = "557058:ada"
	GraceID = "557058:grace"
	LinusID = "557058:linus"
	KenID   = "557058:ken"
)

type writeState struct {
	nextComment int
	failures    []failure
}

type failure struct {
	method, path string
	after        int
}

// Fail makes the fixture answer the method and path (under /2.0) with a 500
// once it has served after successful requests to them.
func (f *Fixture) Fail(method, path string, after int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes.failures = append(f.writes.failures, failure{method: method, path: path, after: after})
}

func (f *Fixture) shouldFail(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.writes.failures {
		fail := &f.writes.failures[i]
		if fail.method != method || fail.path != path {
			continue
		}
		if fail.after == 0 {
			return true
		}
		fail.after--
	}
	return false
}

func (f *Fixture) commentID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes.nextComment++
	return 200 + f.writes.nextComment
}

func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	return string(data)
}

// serveMore answers the listing, identity, activity, and write endpoints, and
// reports whether it handled the request.
func (f *Fixture) serveMore(w http.ResponseWriter, r *http.Request, path, body string) bool {
	if f.shouldFail(r.Method, path) {
		writeError(w, http.StatusInternalServerError, "injected failure")
		return true
	}
	repoPath := fmt.Sprintf("/repositories/%s/%s", Workspace, Repo)
	prPath := fmt.Sprintf("%s/pullrequests/%d", repoPath, PRID)
	get := r.Method == http.MethodGet
	switch {
	case get && path == "/user":
		writeJSON(w, grace)
	case get && strings.HasPrefix(path, "/users/") && userByID[strings.TrimPrefix(path, "/users/")] != "":
		writeJSON(w, userByID[strings.TrimPrefix(path, "/users/")])
	case get && path == "/workspaces/"+Workspace+"/members":
		var members []string
		for _, user := range []string{ada, grace, linus, ken, mentionedUserJSON, samA, samB} {
			members = append(members, `{"user":`+user+`}`)
		}
		writeJSON(w, `{"values":[`+strings.Join(members, ",")+`]}`)
	case get && path == "/repositories/"+Workspace:
		writeJSON(w, `{"values":[{"name":"widgets","full_name":"acme/widgets"},{"name":"Gadgets","full_name":"acme/gadgets"}]}`)
	case get && path == repoPath:
		writeJSON(w, `{"name":"widgets","full_name":"acme/widgets","mainbranch":{"name":"main"}}`)
	case get && path == repoPath+"/pullrequests" && !strings.HasPrefix(r.URL.Query().Get("q"), "source.branch"):
		if states := r.URL.Query()["state"]; len(states) > 0 && !slices.Contains(states, "OPEN") {
			writeJSON(w, `{"values":[]}`)
			return true
		}
		writeJSON(w, `{"values":[`+pullRequestJSON+`,`+draftPullRequestJSON+`]}`)
	case get && path == "/repositories/acme/gadgets/pullrequests":
		writeJSON(w, `{"values":[`+gadgetsPullRequestJSON+`]}`)
	case get && path == prPath+"/activity":
		writeJSON(w, `{"values":[`+activityJSON+`]}`)
	case r.Method == http.MethodPost && path == repoPath+"/pullrequests":
		writeJSON(w, createdPullRequest(body))
	case r.Method == http.MethodPut && path == prPath:
		writeJSON(w, mergePullRequest(pullRequestJSON, body))
	case r.Method == http.MethodPost && path == prPath+"/decline":
		writeJSON(w, mergePullRequest(pullRequestJSON, `{"state":"DECLINED"}`))
	case r.Method == http.MethodPost && path == prPath+"/comments":
		writeJSON(w, f.comment(f.commentID(), body))
	case r.Method == http.MethodPut && strings.HasPrefix(path, prPath+"/comments/"):
		id, err := strconv.Atoi(strings.TrimPrefix(path, prPath+"/comments/"))
		if err != nil {
			return false
		}
		writeJSON(w, f.comment(id, body))
	case r.Method == http.MethodDelete && strings.HasPrefix(path, prPath+"/comments/"):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && path == prPath+"/approve":
		writeJSON(w, reviewResult(true, "approved"))
	case r.Method == http.MethodPost && path == prPath+"/request-changes":
		writeJSON(w, reviewResult(false, "changes_requested"))
	case r.Method == http.MethodDelete && (path == prPath+"/approve" || path == prPath+"/request-changes"):
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

// Two workspace members share the display name Sam Lee.
const (
	samA = `{"display_name":"Sam Lee","nickname":"slee","account_id":"557058:sam-a","uuid":"{sam-a}"}`
	samB = `{"display_name":"Sam  lee","nickname":"samlee","account_id":"557058:sam-b","uuid":"{sam-b}"}`
)

var userByID = map[string]string{
	AdaID:              ada,
	GraceID:            grace,
	LinusID:            linus,
	KenID:              ken,
	"557058:mentioned": mentionedUserJSON,
	"{grace-uuid}":     grace,
	"{margaret-uuid}":  mentionedUserJSON,
	"557058:margaret":  mentionedUserJSON,
}

func (f *Fixture) comment(id int, body string) string {
	var payload map[string]any
	_ = json.Unmarshal([]byte(body), &payload)
	comment := map[string]any{
		"id":         id,
		"content":    map[string]any{"raw": nested(payload, "content", "raw"), "markup": "markdown", "html": ""},
		"user":       json.RawMessage(grace),
		"created_on": "2025-03-06T12:00:00.000000+00:00",
		"updated_on": "2025-03-06T12:00:00.000000+00:00",
		"deleted":    false,
		"links":      map[string]any{"html": map[string]string{"href": fmt.Sprintf("%s#comment-%d", URL, id)}},
	}
	if inline, ok := payload["inline"]; ok {
		comment["inline"] = inline
	}
	if parent, ok := payload["parent"]; ok {
		comment["parent"] = parent
	}
	data, _ := json.Marshal(comment)
	return string(data)
}

func nested(m map[string]any, keys ...string) any {
	var value any = m
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

// mergePullRequest overlays the top-level fields of patch on base.
func mergePullRequest(base, patch string) string {
	var pr, changes map[string]any
	_ = json.Unmarshal([]byte(base), &pr)
	_ = json.Unmarshal([]byte(patch), &changes)
	for key, value := range changes {
		pr[key] = value
	}
	data, _ := json.Marshal(pr)
	return string(data)
}

func createdPullRequest(body string) string {
	created := mergePullRequest(pullRequestJSON, body)
	return mergePullRequest(created, `{"id":44,"description":`+jsonString(nestedString(body, "description"))+`,"links":{"html":{"href":"https://bitbucket.org/acme/widgets/pull-requests/44"}},"participants":[],"comment_count":0,"task_count":0}`)
}

func nestedString(body, key string) string {
	var payload map[string]any
	_ = json.Unmarshal([]byte(body), &payload)
	value, _ := payload[key].(string)
	return value
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func reviewResult(approved bool, state string) string {
	return fmt.Sprintf(`{"type":"participant","user":%s,"role":"REVIEWER","approved":%t,"state":%q,"participated_on":"2025-03-06T12:00:00.000000+00:00"}`, grace, approved, state)
}

const draftPullRequestJSON = `{
  "id": 43,
  "title": "Tidy the docs",
  "description": "",
  "state": "OPEN",
  "draft": true,
  "author": ` + grace + `,
  "source": {"branch": {"name": "docs/tidy"}, "commit": {"hash": "aaa1111"}, "repository": {"full_name": "acme/widgets"}},
  "destination": {"branch": {"name": "main"}, "commit": {"hash": "def5678"}, "repository": {"full_name": "acme/widgets"}},
  "reviewers": [` + ada + `],
  "participants": [],
  "created_on": "2025-03-04T09:00:00.000000+00:00",
  "updated_on": "2025-03-04T10:00:00.000000+00:00",
  "links": {"html": {"href": "https://bitbucket.org/acme/widgets/pull-requests/43"}},
  "comment_count": 0,
  "task_count": 0
}`

const gadgetsPullRequestJSON = `{
  "id": 7,
  "title": "Gadget fix",
  "state": "OPEN",
  "author": ` + linus + `,
  "source": {"branch": {"name": "fix"}, "commit": {"hash": "bbb2222"}, "repository": {"full_name": "acme/gadgets"}},
  "destination": {"branch": {"name": "main"}, "commit": {"hash": "ccc3333"}, "repository": {"full_name": "acme/gadgets"}},
  "reviewers": [],
  "participants": [],
  "created_on": "2025-02-01T09:00:00.000000+00:00",
  "updated_on": "2025-02-02T09:00:00.000000+00:00",
  "links": {"html": {"href": "https://bitbucket.org/acme/gadgets/pull-requests/7"}},
  "comment_count": 1,
  "task_count": 0
}`

// activityJSON is newest first, as Bitbucket returns it.
const activityJSON = `{"approval": {"date": "2025-03-05T16:00:00.000000+00:00", "user": ` + grace + `}},
{"update": {"state": "OPEN", "title": "Fix login: handle 'empty' users", "date": "2025-03-05T15:00:00.000000+00:00", "author": ` + ada + `,
  "source": {"branch": {"name": "feature/login"}, "commit": {"hash": "abc1234"}}, "destination": {"branch": {"name": "main"}, "commit": {"hash": "def5678"}}}},
{"comment": {"id": 103, "content": {"raw": "Please add tests"}, "user": ` + linus + `, "created_on": "2025-03-03T08:00:00.000000+00:00"}},
{"changes_requested": {"date": "2025-03-03T08:05:00.000000+00:00", "user": ` + linus + `}},
{"update": {"state": "OPEN", "title": "Fix login: handle 'empty' users", "date": "2025-03-02T09:00:00.000000+00:00", "author": ` + ada + `,
  "source": {"branch": {"name": "feature/login"}, "commit": {"hash": "9990001"}}, "destination": {"branch": {"name": "main"}, "commit": {"hash": "def5678"}},
  "changes": {"title": {"old": "Fix login", "new": "Fix login: handle 'empty' users"}}}},
{"update": {"state": "OPEN", "title": "Fix login", "date": "2025-03-01T09:00:00.000000+00:00", "author": ` + ada + `,
  "source": {"branch": {"name": "feature/login"}, "commit": {"hash": "9990001"}}, "destination": {"branch": {"name": "main"}, "commit": {"hash": "def5678"}}}}`
