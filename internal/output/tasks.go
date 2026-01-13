package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

type TaskWriter struct {
	w io.Writer
}

func NewTaskWriter(w io.Writer) *TaskWriter {
	return &TaskWriter{w: w}
}

func (tw *TaskWriter) WriteTasks(tasks []bitbucket.Task) error {
	if len(tasks) == 0 {
		return nil
	}

	fmt.Fprintln(tw.w, "## Tasks")
	fmt.Fprintln(tw.w)

	for _, task := range tasks {
		checkbox := "[ ]"
		if task.IsResolved() {
			checkbox = "[x]"
		}
		content := tw.formatContent(task.Content)
		fmt.Fprintf(tw.w, "- %s %s\n", checkbox, content)
	}

	return nil
}

func (tw *TaskWriter) formatContent(content bitbucket.Content) string {
	text := content.Raw
	if text == "" {
		text = content.HTML
	}
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	return text
}
