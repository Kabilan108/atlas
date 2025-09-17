package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/document"
)

func TestFormatFenced(t *testing.T) {
	doc := document.Document{
		Title:     "Sample",
		URL:       "https://example.com",
		ID:        "123",
		Source:    "confluence",
		Space:     "ENG",
		Workspace: "workspace",
		Repo:      "repo",
		Path:      "path/to/item",
		Author:    "Alex",
		UpdatedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Body:      "# Heading\nContent",
	}

	var buf bytes.Buffer
	if err := FormatFenced(&buf, doc); err != nil {
		t.Fatalf("format fenced: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "title: Sample") || !strings.Contains(out, "updated_at: 2024-01-01T12:00:00Z") {
		t.Fatalf("unexpected header: %s", out)
	}
	if !strings.Contains(out, "# Heading") {
		t.Fatalf("missing body: %s", out)
	}
}

func TestFormatXMLish(t *testing.T) {
	doc := document.Document{
		Title: "Sample",
		URL:   "https://example.com",
		ID:    "123",
		Body:  "<p>content</p>",
	}

	var buf bytes.Buffer
	if err := FormatXMLish(&buf, doc); err != nil {
		t.Fatalf("format xmlish: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `title="Sample"`) || !strings.Contains(out, `url="https://example.com"`) {
		t.Fatalf("unexpected attributes: %s", out)
	}
	if !strings.Contains(out, "&lt;p&gt;content&lt;/p&gt;") {
		t.Fatalf("body not escaped: %s", out)
	}
}
