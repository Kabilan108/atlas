// Package snippettest serves a fixed set of Bitbucket snippets so tests of the
// CLI, the snippet ops, and the MCP server can compare output on the same data.
package snippettest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const (
	Workspace = "acme"
	// Empty is a workspace with no snippets.
	Empty = "empty"
	Host  = "api.bitbucket.org"
)

// Request is one request the fixture received. Multipart writes also record
// their form: plain fields in Fields and uploaded files in Files.
type Request struct {
	Method string
	Path   string
	Fields map[string][]string
	Files  map[string]string
}

// String renders the request on one line, with sorted form contents, for
// golden files.
func (r Request) String() string {
	var b strings.Builder
	b.WriteString(r.Method + " " + r.Path)
	for _, key := range sortedKeys(r.Fields) {
		values := append([]string(nil), r.Fields[key]...)
		sort.Strings(values)
		fmt.Fprintf(&b, " %s=%q", key, values)
	}
	for _, name := range sortedKeys(r.Files) {
		fmt.Fprintf(&b, " file[%s]=%q", name, r.Files[name])
	}
	return b.String()
}

type file struct {
	name    string
	content string
}

type snippet struct {
	id, title string
	private   bool
	updated   string
	files     []file
}

var snippets = []snippet{
	{id: "abc123", title: "Deploy notes", private: true, updated: "2024-03-04T05:06:07.000000+00:00", files: []file{
		{"notes.md", "# Deploy\n\nRun the script.\n"},
		{"run.sh", "#!/bin/sh\necho deploy"},
	}},
	{id: "def456", title: "A public query with a title long enough to be truncated", updated: "2024-02-01T00:00:00.000000+00:00", files: []file{
		{"query.sql", "select 1;\n"},
	}},
	{id: "ghi789", title: "Private scratch", private: true, updated: "2024-01-01T00:00:00.000000+00:00", files: []file{
		{"scratch.txt", "scratch\n"},
	}},
}

// Fixture is a fake Bitbucket snippets API for workspace acme.
type Fixture struct {
	mu       sync.Mutex
	requests []Request
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

// RoundTrip serves api.bitbucket.org in-process.
func (f *Fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != Host {
		return nil, fmt.Errorf("snippettest: unexpected host %s", r.URL.Host)
	}
	recorder := httptest.NewRecorder()
	f.ServeHTTP(recorder, r)
	resp := recorder.Result()
	resp.Request = r
	return resp, nil
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := Request{Method: r.Method, Path: r.URL.RequestURI()}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Fields = r.MultipartForm.Value
		req.Files = map[string]string{}
		for _, header := range r.MultipartForm.File["file"] {
			part, err := header.Open()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(part)
			part.Close()
			req.Files[header.Filename] = string(data)
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.EscapedPath(), "/2.0/snippets/")
	parts := strings.SplitN(path, "/", 4)
	for i, part := range parts {
		parts[i], _ = url.PathUnescape(part)
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		f.list(w, r, parts[0])
	case len(parts) == 1 && r.Method == http.MethodPost:
		created := snippet{id: "new001", title: first(req.Fields["title"]), private: first(req.Fields["is_private"]) == "true", updated: "2024-05-01T00:00:00.000000+00:00"}
		for _, name := range sortedKeys(req.Files) {
			created.files = append(created.files, file{name, req.Files[name]})
		}
		writeJSON(w, http.StatusCreated, created.json())
	case len(parts) >= 2 && parts[0] == Workspace:
		s, ok := find(parts[1])
		if !ok {
			http.Error(w, `{"type":"error","error":{"message":"No such snippet"}}`, http.StatusNotFound)
			return
		}
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, s.json())
		case len(parts) == 2 && r.Method == http.MethodPut:
			writeJSON(w, http.StatusOK, s.json())
		case len(parts) == 2 && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case len(parts) == 4 && parts[2] == "files" && r.Method == http.MethodGet:
			for _, file := range s.files {
				if file.name == parts[3] {
					io.WriteString(w, file.content)
					return
				}
			}
			http.Error(w, "no such file", http.StatusNotFound)
		default:
			http.Error(w, "unexpected request", http.StatusNotImplemented)
		}
	default:
		http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
	}
}

func (f *Fixture) list(w http.ResponseWriter, r *http.Request, workspace string) {
	values := []any{}
	if workspace == Workspace {
		role := r.URL.Query().Get("role")
		for _, s := range snippets {
			if role == "owner" && s.id == "def456" {
				continue
			}
			values = append(values, s.json())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": values, "pagelen": len(values)})
}

func find(id string) (snippet, bool) {
	for _, s := range snippets {
		if s.id == id {
			return s, true
		}
	}
	return snippet{}, false
}

func (s snippet) json() map[string]any {
	files := map[string]any{}
	for _, file := range s.files {
		files[file.name] = map[string]any{"links": map[string]any{
			"self": map[string]string{"href": "https://api.bitbucket.org/2.0/snippets/acme/" + s.id + "/files/" + file.name},
		}}
	}
	return map[string]any{
		"id":         s.id,
		"title":      s.title,
		"is_private": s.private,
		"owner":      map[string]string{"display_name": "Ada Lovelace", "uuid": "{ada}", "account_id": "557058:ada"},
		"files":      files,
		"created_on": "2024-01-01T00:00:00.000000+00:00",
		"updated_on": s.updated,
		"links": map[string]any{
			"self": map[string]string{"href": "https://api.bitbucket.org/2.0/snippets/acme/" + s.id},
			"html": map[string]string{"href": "https://bitbucket.org/snippets/acme/" + s.id},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
