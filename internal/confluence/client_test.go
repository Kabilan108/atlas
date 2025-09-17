package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestConfluenceClient_Search(t *testing.T) {
	mockResponse := SearchResult{
		Results: []ContentResult{
			{
				ID:    "123456",
				Type:  "page",
				Title: "Test Page",
				Space: Space{
					Key:  "TEST",
					Name: "Test Space",
				},
				Version: Version{
					Number: 1,
					When:   "2023-01-01T12:00:00.000Z",
					By: User{
						DisplayName: "Test User",
						Email:       "test@example.com",
					},
				},
				Body: Body{
					Storage: Storage{
						Value:          "<h1>Test Content</h1><p>This is a test page.</p>",
						Representation: "storage",
					},
				},
				Links: Links{
					WebUI: "/wiki/spaces/TEST/pages/123456",
					Base:  "https://test.atlassian.net",
				},
			},
		},
		Size: 1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/wiki/rest/api/search") {
			t.Errorf("Expected search endpoint, got %s", r.URL.Path)
		}

		query := r.URL.Query().Get("cql")
		if !strings.Contains(query, "text ~ \"test query\"") {
			t.Errorf("Expected CQL query to contain search term, got %s", query)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	os.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	os.Setenv("ATLASSIAN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ATLASSIAN_EMAIL")
		os.Unsetenv("ATLASSIAN_TOKEN")
	}()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	results, err := client.Search(ctx, "test query", "", false, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Title != "Test Page" {
		t.Errorf("Expected title 'Test Page', got %s", result.Title)
	}

	if result.ID != "123456" {
		t.Errorf("Expected ID '123456', got %s", result.ID)
	}

	if result.Source != "confluence" {
		t.Errorf("Expected source 'confluence', got %s", result.Source)
	}

	if result.Space != "TEST" {
		t.Errorf("Expected space 'TEST', got %s", result.Space)
	}

	expectedURL := "https://test.atlassian.net/wiki/spaces/TEST/pages/123456"
	if result.URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, result.URL)
	}

	if !strings.Contains(result.Content, "Test Content") {
		t.Errorf("Expected content to contain 'Test Content', got %s", result.Content)
	}
}

func TestConfluenceClient_GetContent(t *testing.T) {
	mockContent := ContentResult{
		ID:    "123456",
		Type:  "page",
		Title: "Individual Page",
		Space: Space{
			Key:  "DEMO",
			Name: "Demo Space",
		},
		Version: Version{
			Number: 2,
			When:   "2023-01-02T12:00:00.000Z",
			By: User{
				DisplayName: "Demo User",
				Email:       "demo@example.com",
			},
		},
		Body: Body{
			Storage: Storage{
				Value:          "<h2>Demo Content</h2><p>This is demo content with <strong>formatting</strong>.</p>",
				Representation: "storage",
			},
		},
		Links: Links{
			WebUI: "/wiki/spaces/DEMO/pages/123456",
			Base:  "https://demo.atlassian.net",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/wiki/rest/api/content/123456"
		if !strings.Contains(r.URL.Path, expectedPath) {
			t.Errorf("Expected content endpoint %s, got %s", expectedPath, r.URL.Path)
		}

		expand := r.URL.Query().Get("expand")
		if !strings.Contains(expand, "body.storage") {
			t.Errorf("Expected expand parameter to contain 'body.storage', got %s", expand)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockContent)
	}))
	defer server.Close()

	os.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	os.Setenv("ATLASSIAN_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ATLASSIAN_EMAIL")
		os.Unsetenv("ATLASSIAN_TOKEN")
	}()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	doc, err := client.GetContent(ctx, "123456")
	if err != nil {
		t.Fatalf("GetContent failed: %v", err)
	}

	if doc.Title != "Individual Page" {
		t.Errorf("Expected title 'Individual Page', got %s", doc.Title)
	}

	if doc.ID != "123456" {
		t.Errorf("Expected ID '123456', got %s", doc.ID)
	}

	if doc.Source != "confluence" {
		t.Errorf("Expected source 'confluence', got %s", doc.Source)
	}

	if doc.Space != "DEMO" {
		t.Errorf("Expected space 'DEMO', got %s", doc.Space)
	}

	expectedURL := "https://demo.atlassian.net/wiki/spaces/DEMO/pages/123456"
	if doc.URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, doc.URL)
	}

	if !strings.Contains(doc.Content, "Demo Content") {
		t.Errorf("Expected content to contain 'Demo Content', got %s", doc.Content)
	}

	if !strings.Contains(doc.Content, "**formatting**") {
		t.Errorf("Expected content to contain markdown formatting, got %s", doc.Content)
	}
}

func TestConfluenceClient_BuildCQL(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name     string
		query    string
		space    string
		expected string
	}{
		{
			name:     "Simple query without space",
			query:    "test query",
			space:    "",
			expected: "text ~ \"test query\"",
		},
		{
			name:     "Query with space filter",
			query:    "api documentation",
			space:    "DEV",
			expected: "text ~ \"api documentation\" and space = \"DEV\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.BuildCQL(tt.query, tt.space)
			if result != tt.expected {
				t.Errorf("BuildCQL(%q, %q) = %q, want %q", tt.query, tt.space, result, tt.expected)
			}
		})
	}
}
