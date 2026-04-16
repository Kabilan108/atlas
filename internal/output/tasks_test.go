package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestTaskWriterFormatsTasks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewTaskWriter(&buf)
	writer.SetUserResolver(func(accountID string) (bitbucket.User, bool) {
		if accountID == "acct-123" {
			return bitbucket.User{DisplayName: "Hiep Nguyen"}, true
		}
		return bitbucket.User{}, false
	})

	tasks := []bitbucket.Task{{
		ID:      7,
		State:   "OPEN",
		Content: bitbucket.Content{Raw: "Follow up with @{acct-123}\n\u200c"},
		Comment: bitbucket.Comment{
			User:   bitbucket.User{DisplayName: "Justin Moore"},
			Inline: &bitbucket.Inline{Path: "data_query/app.py", To: intPtr(411)},
		},
	}}

	if err := writer.WriteTasks(tasks); err != nil {
		t.Fatalf("WriteTasks() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"## Tasks",
		"### Task #7 [OPEN]",
		"Author: @Justin Moore",
		"Thread: `data_query/app.py:411`",
		"Follow up with @Hiep Nguyen",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "\u200c") {
		t.Fatalf("output still contains zero-width character:\n%q", output)
	}
}
