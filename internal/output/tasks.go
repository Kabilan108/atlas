package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

type TaskWriter struct {
	w              io.Writer
	resolveMention func(string) (bitbucket.User, bool)
}

func NewTaskWriter(w io.Writer) *TaskWriter {
	return &TaskWriter{w: w}
}

func (tw *TaskWriter) SetUserResolver(resolveMention func(string) (bitbucket.User, bool)) {
	tw.resolveMention = resolveMention
}

func (tw *TaskWriter) WriteTasks(tasks []bitbucket.Task) error {
	if len(tasks) == 0 {
		return nil
	}

	fmt.Fprintln(tw.w, "## Tasks")
	fmt.Fprintln(tw.w)

	for index, task := range tasks {
		if index > 0 {
			fmt.Fprintln(tw.w, "---")
			fmt.Fprintln(tw.w)
		}

		status := "OPEN"
		if task.IsResolved() {
			status = "RESOLVED"
		}

		fmt.Fprintf(tw.w, "### Task #%d [%s]\n\n", task.ID, status)
		if task.Comment.User.IdentityKey() != "" {
			fmt.Fprintf(tw.w, "Author: %s\n", formatUserMention(task.Comment.User))
		}
		if task.Comment.Inline != nil && task.Comment.Inline.Path != "" {
			thread := task.Comment.Inline.Path
			if task.Comment.Inline.To != nil {
				thread = fmt.Sprintf("%s:%d", task.Comment.Inline.Path, *task.Comment.Inline.To)
			} else if task.Comment.Inline.From != nil {
				thread = fmt.Sprintf("%s:%d", task.Comment.Inline.Path, *task.Comment.Inline.From)
			}
			fmt.Fprintf(tw.w, "Thread: `%s`\n", thread)
		}
		fmt.Fprintln(tw.w)
		fmt.Fprintln(tw.w, tw.formatContent(task.Content))
	}

	return nil
}

func (tw *TaskWriter) formatContent(content bitbucket.Content) string {
	text := content.Raw
	if text == "" {
		text = content.HTML
	}
	text = strings.TrimSpace(normalizeMarkdownText(text, tw.resolveMention))
	text = strings.ReplaceAll(text, "\n", " ")
	return text
}
