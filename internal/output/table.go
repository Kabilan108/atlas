package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

type TableWriter struct {
	w       *tabwriter.Writer
	headers []string
}

func NewTableWriter(out io.Writer, headers ...string) *TableWriter {
	tw := &TableWriter{
		w:       tabwriter.NewWriter(out, 0, 0, 2, ' ', 0),
		headers: headers,
	}
	tw.writeRow(headers...)
	return tw
}

func (t *TableWriter) writeRow(cols ...string) {
	fmt.Fprintln(t.w, strings.Join(cols, "\t"))
}

func (t *TableWriter) AddRow(cols ...string) {
	t.writeRow(cols...)
}

func (t *TableWriter) Flush() error {
	return t.w.Flush()
}

func FormatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		return t.Format("Jan 2, 2006")
	}
}

func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
