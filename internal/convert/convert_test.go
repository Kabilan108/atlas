package convert

import (
	"strings"
	"testing"
)

func TestHtmlToMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "Simple paragraph",
			input:    "<p>This is a paragraph</p>",
			expected: "This is a paragraph",
		},
		{
			name:     "Heading",
			input:    "<h1>Main Title</h1>",
			expected: "# Main Title",
		},
		{
			name:     "Bold text",
			input:    "<p>This is <strong>bold</strong> text</p>",
			expected: "This is **bold** text",
		},
		{
			name:     "Italic text",
			input:    "<p>This is <em>italic</em> text</p>",
			expected: "This is _italic_ text",
		},
		{
			name:     "Link",
			input:    "<p>Visit <a href=\"https://example.com\">example</a></p>",
			expected: "Visit [example](https://example.com)",
		},
		{
			name:     "Complex HTML",
			input:    "<h2>Section</h2><p>Some text with <strong>bold</strong> and <a href=\"#\">link</a>.</p>",
			expected: "## Section\n\nSome text with **bold** and link.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := HtmlToMarkdown(tt.input)
			if err != nil {
				t.Errorf("HtmlToMarkdown(%q) error: %v", tt.input, err)
				return
			}

			result = strings.TrimSpace(result)
			if result != tt.expected {
				t.Errorf("HtmlToMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
