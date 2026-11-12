package bitbucket

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

type recordedRequest struct {
	Method      string
	Path        string
	EscapedPath string
	Query       url.Values
	Header      http.Header
	Body        []byte
}

type apiRecorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (r *apiRecorder) add(req recordedRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

func (r *apiRecorder) all() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

type testHandler func(w http.ResponseWriter, r *http.Request, body []byte)

func newRecordingClient(t *testing.T, handler testHandler) (*Client, *apiRecorder, *httptest.Server) {
	t.Helper()
	recorder := &apiRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
		}
		recorder.add(recordedRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			EscapedPath: r.URL.EscapedPath(),
			Query:       r.URL.Query(),
			Header:      r.Header.Clone(),
			Body:        body,
		})
		handler(w, r, body)
	}))
	t.Cleanup(server.Close)
	return newClientForTest(server.URL, server.Client()), recorder, server
}

func writeJSON(w http.ResponseWriter, status int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, payload)
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("request body %q is not JSON: %v", body, err)
	}
	return decoded
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(data)
}

func expectRequest(t *testing.T, got recordedRequest, method, path string) {
	t.Helper()
	if got.Method != method || got.Path != path {
		t.Fatalf("request = %s %s, want %s %s", got.Method, got.Path, method, path)
	}
}
