package bitbucket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetFileContentLimitTruncates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/acme/widgets/src/HEAD/version.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"version":"1.2.3"}`))
	}))
	defer server.Close()
	client := newClientForTest(server.URL, server.Client())

	body, err := client.GetFileContentLimit(context.Background(), "acme", "widgets", "HEAD", "version.json", 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(body.Data) != `{"ver` || !body.Truncated {
		t.Fatalf("body = %q truncated=%v", body.Data, body.Truncated)
	}
	body, err = client.GetFileContentLimit(context.Background(), "acme", "widgets", "HEAD", "version.json", 19)
	if err != nil || body.Truncated || string(body.Data) != `{"version":"1.2.3"}` {
		t.Fatalf("exact-limit body = %+v, %v", body, err)
	}
	if _, err := client.GetFileContentLimit(context.Background(), "acme", "widgets", "HEAD", "version.json", -1); err == nil {
		t.Fatal("negative limit succeeded, want an error")
	}
	_, err = client.GetFileContentLimit(context.Background(), "acme", "widgets", "HEAD", "missing.json", 100)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "HEAD:missing.json") {
		t.Fatalf("err = %v, want not found naming the file", err)
	}
}

func TestGetAPIRefusesPathsOutsideBase(t *testing.T) {
	client := newClientForTest("https://api.bitbucket.org/2.0", http.DefaultClient)
	for _, path := range []string{"/../user", "@evil.example/x", "/repositories/%2e%2e/x"} {
		if _, err := client.GetAPI(context.Background(), path, 10); err == nil {
			t.Errorf("GetAPI(%q) succeeded", path)
		}
	}
}

func TestGetPullRequestDiffLimitStopsReading(t *testing.T) {
	const size = 64 << 20
	var served int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/acme/widgets/pullrequests/42/diff" {
			http.NotFound(w, r)
			return
		}
		chunk := []byte(strings.Repeat("+line\n", 1024))
		for served < size {
			n, err := w.Write(chunk)
			served += int64(n)
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := newClientForTest(server.URL, server.Client())

	body, err := client.GetPullRequestDiffLimit(context.Background(), "acme", "widgets", 42, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !body.Truncated || len(body.Data) != 1<<20 {
		t.Fatalf("got %d bytes, truncated=%v", len(body.Data), body.Truncated)
	}
	if served >= size {
		t.Fatalf("server wrote the whole %d-byte diff; the client should stop reading at the limit", size)
	}
}
