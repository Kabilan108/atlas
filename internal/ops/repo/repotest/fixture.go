// Package repotest serves a fixed Bitbucket workspace (repositories,
// branches, commits, files, members) for tests of the repo ops and the CLI.
package repotest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const (
	Workspace = "acme"
	Host      = "api.bitbucket.org"
	// Broken is a repository outside Repos whose files fail with a 500.
	Broken = "broken"
)

// Files holds version.json per repository; a repository missing here has no
// such file, and "blob" holds binary data.
var Files = map[string]string{
	"widgets": `{"version":"1.2.3"}`,
	"gadgets": `{"version":"2.0.0"}` + "\n",
	"blob":    "\x00\x01binary",
}

// Repos is every repository in acme, most recently updated first.
var Repos = []string{"widgets", "gadgets", "legacy", "blob"}

// Fixture is the fake API. It records each request's method and URI.
type Fixture struct {
	mu       sync.Mutex
	requests []string
}

func New() *Fixture {
	return &Fixture{}
}

// Requests returns "METHOD /uri" for each request served so far.
func (f *Fixture) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// RoundTrip serves api.bitbucket.org in-process.
func (f *Fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != Host {
		return nil, fmt.Errorf("repotest: unexpected host %s", r.URL.Host)
	}
	recorder := httptest.NewRecorder()
	f.ServeHTTP(recorder, r)
	resp := recorder.Result()
	resp.Request = r
	return resp, nil
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.mu.Unlock()
	if r.Method != http.MethodGet {
		http.Error(w, "read-only fixture", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/2.0")
	switch {
	case path == "/user":
		writeJSON(w, user("Ada Lovelace", "ada", "557058:ada", "{ada}"))
	case path == "/workspaces/acme/members":
		writeJSON(w, page(
			map[string]any{"user": user("Ada Lovelace", "ada", "557058:ada", "{ada}")},
			map[string]any{"user": user("Grace Hopper", "grace", "557058:grace", "{grace}")},
		))
	case path == "/repositories/acme":
		var values []any
		for i, name := range Repos {
			if q := r.URL.Query().Get("q"); q != "" && !strings.Contains(name, strings.Trim(strings.TrimPrefix(q, "name ~ "), `"`)) {
				continue
			}
			values = append(values, map[string]any{
				"name": name, "full_name": "acme/" + name, "is_private": name != "gadgets",
				"description": "The " + name + " service\nsecond line",
				"mainbranch":  map[string]string{"name": "main"},
				"updated_on":  fmt.Sprintf("2024-06-%02dT00:00:00+00:00", 20-i),
			})
		}
		writeJSON(w, page(values...))
	case strings.HasPrefix(path, "/repositories/acme/"):
		f.serveRepo(w, r, strings.SplitN(strings.TrimPrefix(path, "/repositories/acme/"), "/", 2))
	default:
		http.NotFound(w, r)
	}
}

func (f *Fixture) serveRepo(w http.ResponseWriter, r *http.Request, parts []string) {
	repo, rest := parts[0], ""
	if len(parts) == 2 {
		rest = parts[1]
	}
	switch {
	case rest == "refs/branches":
		writeJSON(w, page(branch("main", "a1b2c3d4e5f6a7b8", "Release 1.2.3"), branch("feature/login", "0f0e0d0c0b0a0908", "Add login\n\nDetails")))
	case rest == "refs/branches/main":
		main := branch("main", "a1b2c3d4e5f6a7b8", "Release 1.2.3")
		main["default_merge_strategy"] = "squash"
		main["merge_strategies"] = []string{"merge_commit", "squash"}
		writeJSON(w, main)
	case strings.HasPrefix(rest, "commits"):
		writeJSON(w, page(commit("a1b2c3d4e5f6a7b8", "Release 1.2.3"), commit("99887766554433", "Fix | pipes")))
	case repo == Broken:
		http.Error(w, `{"type":"error","error":{"message":"Something went wrong"}}`, http.StatusInternalServerError)
	case rest == "src/HEAD/version.json" || rest == "src/v1/version.json":
		content, ok := Files[repo]
		if !ok {
			http.Error(w, `{"type":"error","error":{"message":"No such file or directory: version.json"}}`, http.StatusNotFound)
			return
		}
		w.Write([]byte(content))
	default:
		http.Error(w, `{"type":"error","error":{"message":"not found"}}`, http.StatusNotFound)
	}
}

func user(name, nickname, accountID, uuid string) map[string]string {
	return map[string]string{"display_name": name, "nickname": nickname, "account_id": accountID, "uuid": uuid}
}

func branch(name, hash, message string) map[string]any {
	return map[string]any{"name": name, "target": commit(hash, message)}
}

func commit(hash, message string) map[string]any {
	return map[string]any{
		"hash": hash, "message": message, "date": "2024-06-01T12:30:00+00:00",
		"author": map[string]any{"raw": "Ada Lovelace <ada@example.org>"},
	}
}

func page(values ...any) map[string]any {
	if values == nil {
		values = []any{}
	}
	return map[string]any{"values": values}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
