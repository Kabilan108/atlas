// Package jiratest is a fake Jira for op and CLI tests: routes answer by
// method and path, and every request is recorded.
package jiratest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Site is the base URL the fake's serverInfo reports.
const Site = "https://acme.atlassian.net"

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

// Fake serves registered routes. GET /rest/api/3/serverInfo answers by
// default; anything unregistered is a 404 with a Jira error body.
type Fake struct {
	mu       sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []Request
}

func New() *Fake {
	f := &Fake{routes: map[string]http.HandlerFunc{}}
	f.JSON("GET /rest/api/3/serverInfo", http.StatusOK, `{"baseUrl":"`+Site+`","deploymentType":"Cloud"}`)
	return f
}

// Handle registers handler for "METHOD /path".
func (f *Fake) Handle(route string, handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[route] = handler
}

// JSON registers a fixed JSON response.
func (f *Fake) JSON(route string, status int, body string) {
	f.Handle(route, func(w http.ResponseWriter, _ *http.Request) {
		Write(w, status, body)
	})
}

// Write sends a JSON response.
func Write(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

// Requests returns what the fake received, in order.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Find returns the requests for one route.
func (f *Fake) Find(route string) []Request {
	method, path, _ := strings.Cut(route, " ")
	var out []Request
	for _, r := range f.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// Routes lists "METHOD /path" for every request, in order.
func (f *Fake) Routes() []string {
	var out []string
	for _, r := range f.Requests() {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	route := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body), Header: r.Header.Clone()})
	handler := f.routes[route]
	f.mu.Unlock()
	if handler == nil {
		Write(w, http.StatusNotFound, fmt.Sprintf(`{"errorMessages":["jiratest: no route %s"]}`, route))
		return
	}
	handler(w, r)
}

// Transport serves the fake to a client pointed at the Atlassian gateway
// (https://api.atlassian.com/ex/jira/{cloudId}/...), as the CLI's is.
func (f *Fake) Transport() http.RoundTripper {
	return roundTripper(func(r *http.Request) (*http.Response, error) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/ex/jira/"), "/", 2)
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		if len(parts) == 2 {
			clone.URL.Path += parts[1]
		}
		clone.RequestURI = clone.URL.RequestURI()
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, clone)
		return rec.Result(), nil
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (rt roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}
