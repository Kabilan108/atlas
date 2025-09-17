package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Document struct {
	Title     string
	URL       string
	ID        string
	Source    string
	Space     string
	Workspace string
	Repo      string
	Path      string
	Author    string
	UpdatedAt string
	Content   string
}

type Format string

const (
	FormatFenced Format = "fenced"
	FormatXMLish Format = "xmlish"
)

func WriteDocument(doc *Document, format Format) error {
	switch format {
	case FormatFenced:
		return writeFenced(doc, os.Stdout)
	case FormatXMLish:
		return writeXMLish(doc, os.Stdout)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func writeFenced(doc *Document, w io.Writer) error {
	var header strings.Builder

	if doc.Title != "" {
		header.WriteString(fmt.Sprintf("title: %s\n", doc.Title))
	}
	if doc.URL != "" {
		header.WriteString(fmt.Sprintf("url: %s\n", doc.URL))
	}
	if doc.ID != "" {
		header.WriteString(fmt.Sprintf("id: %s\n", doc.ID))
	}
	if doc.Source != "" {
		header.WriteString(fmt.Sprintf("source: %s\n", doc.Source))
	}
	if doc.Space != "" {
		header.WriteString(fmt.Sprintf("space: %s\n", doc.Space))
	}
	if doc.Workspace != "" {
		header.WriteString(fmt.Sprintf("workspace: %s\n", doc.Workspace))
	}
	if doc.Repo != "" {
		header.WriteString(fmt.Sprintf("repo: %s\n", doc.Repo))
	}
	if doc.Path != "" {
		header.WriteString(fmt.Sprintf("path: %s\n", doc.Path))
	}
	if doc.Author != "" {
		header.WriteString(fmt.Sprintf("author: %s\n", doc.Author))
	}
	if doc.UpdatedAt != "" {
		header.WriteString(fmt.Sprintf("updated_at: %s\n", doc.UpdatedAt))
	}

	_, err := fmt.Fprintf(w, "```yaml\n%s```\n\n%s\n", header.String(), doc.Content)
	return err
}

func writeXMLish(doc *Document, w io.Writer) error {
	var attrs strings.Builder

	if doc.URL != "" {
		attrs.WriteString(fmt.Sprintf(` url="%s"`, escapeXMLAttribute(doc.URL)))
	}
	if doc.Title != "" {
		attrs.WriteString(fmt.Sprintf(` title="%s"`, escapeXMLAttribute(doc.Title)))
	}
	if doc.ID != "" {
		attrs.WriteString(fmt.Sprintf(` id="%s"`, escapeXMLAttribute(doc.ID)))
	}
	if doc.Source != "" {
		attrs.WriteString(fmt.Sprintf(` source="%s"`, escapeXMLAttribute(doc.Source)))
	}
	if doc.Space != "" {
		attrs.WriteString(fmt.Sprintf(` space="%s"`, escapeXMLAttribute(doc.Space)))
	}
	if doc.Workspace != "" {
		attrs.WriteString(fmt.Sprintf(` workspace="%s"`, escapeXMLAttribute(doc.Workspace)))
	}
	if doc.Repo != "" {
		attrs.WriteString(fmt.Sprintf(` repo="%s"`, escapeXMLAttribute(doc.Repo)))
	}
	if doc.Path != "" {
		attrs.WriteString(fmt.Sprintf(` path="%s"`, escapeXMLAttribute(doc.Path)))
	}
	if doc.Author != "" {
		attrs.WriteString(fmt.Sprintf(` author="%s"`, escapeXMLAttribute(doc.Author)))
	}
	if doc.UpdatedAt != "" {
		attrs.WriteString(fmt.Sprintf(` updated_at="%s"`, escapeXMLAttribute(doc.UpdatedAt)))
	}

	_, err := fmt.Fprintf(w, "<document%s>%s</document>\n", attrs.String(), doc.Content)
	return err
}

func escapeXMLAttribute(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func LogError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

func LogInfo(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Info: "+format+"\n", args...)
}

func LogVerbose(verbose bool, format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "Verbose: "+format+"\n", args...)
	}
}

func FormatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}
