package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteFenced(t *testing.T) {
	doc := &Document{
		Title:     "Test Document",
		URL:       "https://example.com/page/123",
		ID:        "123",
		Source:    "confluence",
		Space:     "TEST",
		Author:    "Test Author",
		UpdatedAt: "2023-01-01T12:00:00.000Z",
		Content:   "# Test Content\n\nThis is test content.",
	}

	var buf bytes.Buffer
	err := writeFenced(doc, &buf)
	if err != nil {
		t.Fatalf("writeFenced failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "```yaml") {
		t.Error("Expected yaml code block in fenced output")
	}

	if !strings.Contains(output, "title: Test Document") {
		t.Error("Expected title in fenced output")
	}

	if !strings.Contains(output, "source: confluence") {
		t.Error("Expected source in fenced output")
	}

	if !strings.Contains(output, "# Test Content") {
		t.Error("Expected content in fenced output")
	}
}

func TestWriteXMLish(t *testing.T) {
	doc := &Document{
		Title:   "Test Document",
		URL:     "https://example.com/page/123",
		ID:      "123",
		Source:  "bitbucket",
		Content: "Test content with **markdown**",
	}

	var buf bytes.Buffer
	err := writeXMLish(doc, &buf)
	if err != nil {
		t.Fatalf("writeXMLish failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "<document") {
		t.Error("Expected document tag in xmlish output")
	}

	if !strings.Contains(output, "title=\"Test Document\"") {
		t.Error("Expected title attribute in xmlish output")
	}

	if !strings.Contains(output, "source=\"bitbucket\"") {
		t.Error("Expected source attribute in xmlish output")
	}

	if !strings.Contains(output, "</document>") {
		t.Error("Expected closing document tag in xmlish output")
	}

	if !strings.Contains(output, "Test content with **markdown**") {
		t.Error("Expected content in xmlish output")
	}
}

func TestEscapeXMLAttribute(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "simple text",
			expected: "simple text",
		},
		{
			input:    "text with \"quotes\"",
			expected: "text with &quot;quotes&quot;",
		},
		{
			input:    "text with <tags>",
			expected: "text with &lt;tags&gt;",
		},
		{
			input:    "text with & ampersand",
			expected: "text with &amp; ampersand",
		},
		{
			input:    "mixed \"quotes\" & <tags>",
			expected: "mixed &quot;quotes&quot; &amp; &lt;tags&gt;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeXMLAttribute(tt.input)
			if result != tt.expected {
				t.Errorf("escapeXMLAttribute(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
