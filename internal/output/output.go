package output

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/document"
)

// Format defines the available output wrappers.
type Format string

const (
	// Fenced produces a markdown fenced block.
	Fenced Format = "fenced"
	// XMLish produces a lightweight XML-inspired wrapper.
	XMLish Format = "xmlish"
)

// PrintFenced writes a fenced markdown block to stdout.
func PrintFenced(doc document.Document) error {
	return FormatFenced(os.Stdout, doc)
}

// PrintXMLish writes an XML-like structure to stdout.
func PrintXMLish(doc document.Document) error {
	return FormatXMLish(os.Stdout, doc)
}

// FormatFenced renders the document in a fenced markdown block to the provided writer.
func FormatFenced(w io.Writer, doc document.Document) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}

	fields := []struct {
		key   string
		value string
	}{
		{"title", doc.Title},
		{"url", doc.URL},
		{"id", doc.ID},
		{"source", doc.Source},
		{"space", doc.Space},
		{"workspace", doc.Workspace},
		{"repo", doc.Repo},
		{"path", doc.Path},
		{"author", doc.Author},
		{"updated_at", formatTime(doc.UpdatedAt)},
	}

	if _, err := fmt.Fprintln(w, "```markdown"); err != nil {
		return err
	}

	for _, field := range fields {
		if _, err := fmt.Fprintf(w, "%s: %s\n", field.key, field.value); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, "---"); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, strings.TrimRight(doc.Body, "\n")); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "```")
	return err
}

// FormatXMLish renders the document as a pseudo-XML element.
func FormatXMLish(w io.Writer, doc document.Document) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}

	attrs := []string{
		attr("title", doc.Title),
		attr("url", doc.URL),
		attr("id", doc.ID),
		attr("source", doc.Source),
		attr("space", doc.Space),
		attr("workspace", doc.Workspace),
		attr("repo", doc.Repo),
		attr("path", doc.Path),
		attr("author", doc.Author),
		attr("updated_at", formatTime(doc.UpdatedAt)),
	}

	body := escapeString(doc.Body)

	_, err := fmt.Fprintf(w, "<document %s>%s</document>\n", strings.Join(attrs, " "), body)
	return err
}

func attr(key, value string) string {
	return fmt.Sprintf(`%s="%s"`, key, escapeString(value))
}

func escapeString(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
