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
	w              io.Writer
	prAuthor       bitbucket.User
	converter      *md.Converter
	diffParser     *DiffParser
	resolveMention func(string) (bitbucket.User, bool)
}

func NewCommentWriter(w io.Writer, prAuthor bitbucket.User) *CommentWriter {
	return &CommentWriter{
		w:         w,
		prAuthor:  prAuthor,
		converter: md.NewConverter("", true, nil),
	}
}

func (cw *CommentWriter) SetUserResolver(resolveMention func(string) (bitbucket.User, bool)) {
	cw.resolveMention = resolveMention
}

func (cw *CommentWriter) SetDiff(diff []byte) {
	cw.diffParser = NewDiffParser()
	cw.diffParser.Parse(diff)
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
	for _, comment := range comments {
		if comment.Deleted {
			continue
		}
		if !includeResolved && comment.IsResolved() {
			continue
		}
		filtered = append(filtered, comment)
	}
	return filtered
}

type locationKey struct {
	path string
	line int
}

func (cw *CommentWriter) groupByLocation(comments []bitbucket.Comment) map[locationKey][]bitbucket.Comment {
	grouped := make(map[locationKey][]bitbucket.Comment)

	for _, comment := range comments {
		if comment.Parent != nil {
			continue
		}

		key := locationKey{}
		if comment.Inline != nil {
			key.path = comment.Inline.Path
			if comment.Inline.To != nil {
				key.line = *comment.Inline.To
			} else if comment.Inline.From != nil {
				key.line = *comment.Inline.From
			}
		}

		grouped[key] = append(grouped[key], comment)
	}

	return grouped
}

func (cw *CommentWriter) writeGroupedComments(grouped map[locationKey][]bitbucket.Comment, allComments []bitbucket.Comment) {
	var keys []locationKey
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		return keys[i].line < keys[j].line
	})

	fmt.Fprintln(cw.w, "## Comments")
	fmt.Fprintln(cw.w)

	for index, key := range keys {
		if index > 0 {
			fmt.Fprintln(cw.w, "---")
			fmt.Fprintln(cw.w)
		}

		parents := grouped[key]
		sort.Slice(parents, func(i, j int) bool {
			if !parents[i].CreatedOn.Equal(parents[j].CreatedOn) {
				return parents[i].CreatedOn.Before(parents[j].CreatedOn)
			}
			return parents[i].ID < parents[j].ID
		})

		fmt.Fprintln(cw.w, cw.threadHeader(key, parents))
		fmt.Fprintln(cw.w)

		if key.path != "" && cw.diffParser != nil && key.line > 0 {
			hunk := cw.diffParser.GetHunkForLine(key.path, key.line)
			if hunk != nil {
				fmt.Fprint(cw.w, hunk.FormatContext(key.line, 3))
				fmt.Fprintln(cw.w)
			}
		}

		for parentIndex, parent := range parents {
			if parentIndex > 0 {
				fmt.Fprintln(cw.w)
			}
			cw.writeComment(parent, false)

			for _, reply := range cw.repliesFor(parent.ID, allComments) {
				fmt.Fprintln(cw.w)
				cw.writeComment(reply, true)
			}
		}
	}
}

func (cw *CommentWriter) repliesFor(parentID int, comments []bitbucket.Comment) []bitbucket.Comment {
	var replies []bitbucket.Comment
	for _, comment := range comments {
		if comment.Parent != nil && comment.Parent.ID == parentID {
			replies = append(replies, comment)
		}
	}

	sort.Slice(replies, func(i, j int) bool {
		if !replies[i].CreatedOn.Equal(replies[j].CreatedOn) {
			return replies[i].CreatedOn.Before(replies[j].CreatedOn)
		}
		return replies[i].ID < replies[j].ID
	})

	return replies
}

func (cw *CommentWriter) threadHeader(key locationKey, comments []bitbucket.Comment) string {
	location := "General"
	if key.path != "" {
		if key.line > 0 {
			location = fmt.Sprintf("`%s:%d`", key.path, key.line)
		} else {
			location = fmt.Sprintf("`%s`", key.path)
		}
	}

	return fmt.Sprintf("### Thread: %s [%s]", location, threadStatus(comments))
}

func threadStatus(comments []bitbucket.Comment) string {
	hasResolved := false
	for _, comment := range comments {
		if comment.IsResolved() {
			hasResolved = true
			continue
		}
		if comment.Inline != nil {
			return "UNRESOLVED"
		}
	}
	if hasResolved {
		return "RESOLVED"
	}
	return "OPEN"
}

func (cw *CommentWriter) writeComment(comment bitbucket.Comment, isReply bool) {
	commentType := "comment"
	if isReply {
		commentType = "reply"
	}

	authorIndicator := ""
	if comment.User.SharesStableIdentity(cw.prAuthor) {
		authorIndicator = " (author)"
	}

	fmt.Fprintf(cw.w, "Type: %s\n", commentType)
	fmt.Fprintf(cw.w, "ID: %d\n", comment.ID)
	fmt.Fprintf(cw.w, "Author: %s%s\n", formatUserMention(comment.User), authorIndicator)
	fmt.Fprintf(cw.w, "When: %s\n", cw.formatTimestamp(comment.CreatedOn))
	if status := commentStatus(comment); status != "" {
		fmt.Fprintf(cw.w, "Status: %s\n", status)
	}
	fmt.Fprintln(cw.w)

	content := cw.convertContent(comment.Content)
	for _, line := range strings.Split(content, "\n") {
		fmt.Fprintln(cw.w, line)
	}
}

func commentStatus(comment bitbucket.Comment) string {
	if comment.IsResolved() {
		return "RESOLVED"
	}
	if comment.Inline != nil {
		return "UNRESOLVED"
	}
	return ""
}

func (cw *CommentWriter) convertContent(content bitbucket.Content) string {
	if content.HTML != "" {
		converted, err := cw.converter.ConvertString(content.HTML)
		if err == nil {
			return strings.TrimSpace(normalizeMarkdownText(converted, cw.resolveMention))
		}
	}
	if content.Raw != "" {
		return strings.TrimSpace(normalizeMarkdownText(content.Raw, cw.resolveMention))
	}
	return ""
}

func (cw *CommentWriter) formatTimestamp(timestamp time.Time) string {
	relative := FormatRelativeTime(timestamp)
	absolute := timestamp.Format("2006-01-02 15:04")
	return fmt.Sprintf("%s - %s", relative, absolute)
}
