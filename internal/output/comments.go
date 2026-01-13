package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/kabilan108/atlas/internal/bitbucket"
)

type CommentWriter struct {
	w          io.Writer
	prAuthorID string
	converter  *md.Converter
}

func NewCommentWriter(w io.Writer, prAuthorID string) *CommentWriter {
	return &CommentWriter{
		w:          w,
		prAuthorID: prAuthorID,
		converter:  md.NewConverter("", true, nil),
	}
}

func (cw *CommentWriter) WriteComments(comments []bitbucket.Comment, includeResolved bool) error {
	filtered := cw.filterComments(comments, includeResolved)
	if len(filtered) == 0 {
		fmt.Fprintln(cw.w, "No comments.")
		return nil
	}

	grouped := cw.groupByLocation(filtered)
	cw.writeGroupedComments(grouped, filtered)
	return nil
}

func (cw *CommentWriter) filterComments(comments []bitbucket.Comment, includeResolved bool) []bitbucket.Comment {
	var filtered []bitbucket.Comment
	for _, c := range comments {
		if c.Deleted {
			continue
		}
		if !includeResolved && c.IsResolved() {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

type locationKey struct {
	path string
	line int
}

func (cw *CommentWriter) groupByLocation(comments []bitbucket.Comment) map[locationKey][]bitbucket.Comment {
	grouped := make(map[locationKey][]bitbucket.Comment)

	commentMap := make(map[int]bitbucket.Comment)
	for _, c := range comments {
		commentMap[c.ID] = c
	}

	for _, c := range comments {
		if c.Parent != nil {
			continue
		}

		key := locationKey{}
		if c.Inline != nil {
			key.path = c.Inline.Path
			if c.Inline.To != nil {
				key.line = *c.Inline.To
			} else if c.Inline.From != nil {
				key.line = *c.Inline.From
			}
		}

		grouped[key] = append(grouped[key], c)
	}

	return grouped
}

func (cw *CommentWriter) writeGroupedComments(grouped map[locationKey][]bitbucket.Comment, allComments []bitbucket.Comment) {
	commentMap := make(map[int]bitbucket.Comment)
	for _, c := range allComments {
		commentMap[c.ID] = c
	}

	var keys []locationKey
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		return keys[i].line < keys[j].line
	})

	fmt.Fprintln(cw.w, "## Comments")
	fmt.Fprintln(cw.w)

	for _, key := range keys {
		comments := grouped[key]

		if key.path != "" {
			fmt.Fprintf(cw.w, "#### `%s", key.path)
			if key.line > 0 {
				fmt.Fprintf(cw.w, ":%d", key.line)
			}
			fmt.Fprintln(cw.w, "`")
			fmt.Fprintln(cw.w)
		}

		for _, parent := range comments {
			cw.writeComment(parent, 0)

			for _, c := range allComments {
				if c.Parent != nil && c.Parent.ID == parent.ID {
					cw.writeComment(c, 1)
				}
			}
		}
	}
}

func (cw *CommentWriter) writeComment(c bitbucket.Comment, depth int) {
	indent := ""
	if depth > 0 {
		indent = "> "
	}

	authorIndicator := ""
	if c.User.UUID == cw.prAuthorID || c.User.AccountID == cw.prAuthorID {
		authorIndicator = " (author)"
	}

	status := ""
	if c.IsResolved() {
		status = " [RESOLVED]"
	} else if c.Inline != nil {
		status = " [UNRESOLVED]"
	}

	timestamp := cw.formatTimestamp(c.CreatedOn)

	fmt.Fprintf(cw.w, "%s**@%s**%s (%s)%s:\n", indent, c.User.Username, authorIndicator, timestamp, status)

	content := cw.convertContent(c.Content)
	for _, line := range strings.Split(content, "\n") {
		fmt.Fprintf(cw.w, "%s%s\n", indent, line)
	}
	fmt.Fprintln(cw.w)
}

func (cw *CommentWriter) convertContent(content bitbucket.Content) string {
	if content.HTML != "" {
		converted, err := cw.converter.ConvertString(content.HTML)
		if err == nil {
			return strings.TrimSpace(converted)
		}
	}
	if content.Raw != "" {
		return strings.TrimSpace(content.Raw)
	}
	return ""
}

func (cw *CommentWriter) formatTimestamp(t time.Time) string {
	relative := FormatRelativeTime(t)
	absolute := t.Format("2006-01-02 15:04")
	return fmt.Sprintf("%s - %s", relative, absolute)
}
