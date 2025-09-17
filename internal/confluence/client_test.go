package confluence

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/httpclient"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetPage(t *testing.T) {
	t.Setenv(baseURLEnv, "https://example.test")

	handler := func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/wiki/rest/api/content/123" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if want := "body.storage,space,version"; r.URL.Query().Get("expand") != want {
			t.Fatalf("expected expand=%s, got %s", want, r.URL.Query().Get("expand"))
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Fatalf("missing authorization header")
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err != nil {
			t.Fatalf("invalid auth header: %v", err)
		}
		if string(decoded) != "tester:secret" {
			t.Fatalf("unexpected credentials: %s", decoded)
		}

		body := `{
            "id": "123",
            "title": "Sample Page",
            "body": {"storage": {"value": "<h1>Hello</h1><p>Welcome</p>"}},
            "space": {"key": "ENG", "name": "Engineering"},
            "version": {"number": 2, "when": "2024-01-02T15:04:05.000+0000", "by": {"displayName": "Jane Doe"}},
            "_links": {"webui": "/spaces/ENG/pages/123/Sample+Page"}
        }`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}

	hc, err := httpclient.New(
		httpclient.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}),
		httpclient.WithCredentials("tester", "secret"),
	)
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	client, err := NewClient(hc, config.Config{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	page, err := client.GetPage(context.Background(), "123")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}

	if page.ID != "123" || page.Title != "Sample Page" {
		t.Fatalf("unexpected page metadata: %+v", page)
	}

	if page.SpaceKey != "ENG" || page.SpaceName != "Engineering" {
		t.Fatalf("unexpected space info: %+v", page)
	}

	if !strings.Contains(page.Markdown, "# Hello") {
		t.Fatalf("markdown conversion failed: %q", page.Markdown)
	}

	expectedURL := "https://example.test/spaces/ENG/pages/123/Sample+Page"
	if page.WebURL != expectedURL {
		t.Fatalf("expected web url %s, got %s", expectedURL, page.WebURL)
	}

	wantTime, _ := time.Parse("2006-01-02T15:04:05.000-0700", "2024-01-02T15:04:05.000+0000")
	if !page.Updated.Equal(wantTime) {
		t.Fatalf("unexpected updated time: %s", page.Updated)
	}

	if page.Author != "Jane Doe" {
		t.Fatalf("expected author Jane Doe, got %s", page.Author)
	}

	if page.Source != confluenceSourceLabel {
		t.Fatalf("expected source %s, got %s", confluenceSourceLabel, page.Source)
	}
}

func TestSearchPages(t *testing.T) {
	t.Setenv(baseURLEnv, "https://example.test")

	var receivedCQL string
	handler := func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/wiki/rest/api/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		receivedCQL = r.URL.Query().Get("cql")
		if r.URL.Query().Get("limit") != "5" {
			t.Fatalf("expected limit 5")
		}
		body := `{
            "results": [
                {"content": {"id": "1", "title": "First", "space": {"key": "ENG", "name": "Engineering"}, "_links": {"webui": "/spaces/ENG/pages/1/First"}}},
                {"content": {"id": "2", "title": "Second", "space": {"key": "DOC", "name": "Docs"}, "_links": {"webui": "/spaces/DOC/pages/2/Second"}}}
            ]
        }`
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}

	hc, err := httpclient.New(
		httpclient.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}),
		httpclient.WithCredentials("tester", "secret"),
	)
	if err != nil {
		t.Fatalf("http client: %v", err)
	}

	client, err := NewClient(hc, config.Config{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	opts := SearchOptions{Query: "release notes", Space: "ENG", Limit: 5}
	results, err := client.SearchPages(context.Background(), opts)
	if err != nil {
		t.Fatalf("search pages: %v", err)
	}

	expectedCQL := `space = "ENG" AND text ~ "release notes"`
	if receivedCQL != expectedCQL {
		t.Fatalf("unexpected cql query: %s", receivedCQL)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].ID != "1" || results[0].WebURL != "https://example.test/spaces/ENG/pages/1/First" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}

	if results[0].Source != confluenceSourceLabel {
		t.Fatalf("unexpected source: %s", results[0].Source)
	}
}

func TestBuildCQL(t *testing.T) {
	cases := []struct {
		name    string
		opts    SearchOptions
		want    string
		wantErr bool
	}{
		{name: "empty", opts: SearchOptions{}, wantErr: true},
		{name: "cql passthrough", opts: SearchOptions{Query: "type=page", CQL: true}, want: "type=page"},
		{name: "quoted", opts: SearchOptions{Query: `roadmap "2024"`}, want: `text ~ "roadmap \"2024\""`},
		{name: "space scoped", opts: SearchOptions{Query: "release", Space: "ENG"}, want: `space = "ENG" AND text ~ "release"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCQL(tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
