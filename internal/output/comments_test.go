package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestCommentWriterFormatsThreadsAndMetadata(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewCommentWriter(&buf, bitbucket.User{UUID: "{author-1}"})
	comments := []bitbucket.Comment{
		{
			ID:        1,
			User:      bitbucket.User{UUID: "{author-1}", DisplayName: "Tony Okeke"},
			Content:   bitbucket.Content{Raw: "Author comment"},
			CreatedOn: time.Date(2026, time.March, 10, 14, 3, 0, 0, time.UTC),
		},
		{
			ID:        2,
			User:      bitbucket.User{UUID: "{reviewer-1}", DisplayName: "Justin Moore"},
			Content:   bitbucket.Content{Raw: "Reviewer comment"},
			CreatedOn: time.Date(2026, time.March, 10, 14, 4, 0, 0, time.UTC),
			Inline:    &bitbucket.Inline{Path: "data_query/app.py", To: intPtr(411)},
		},
	}

	if err := writer.WriteComments(comments, false); err != nil {
		t.Fatalf("WriteComments() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"## Comments",
		"### Thread: General [OPEN]",
		"### Thread: `data_query/app.py:411` [UNRESOLVED]",
		"Type: comment",
		"Author: @Tony Okeke (author)",
		"Author: @Justin Moore",
		"Status: UNRESOLVED",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}

func TestCommentWriterEscapesMarkdownNamesAndUsesStableAuthorFallback(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewCommentWriter(&buf, bitbucket.User{AccountID: "acct-123"})
	comments := []bitbucket.Comment{{
		ID:        1,
		User:      bitbucket.User{AccountID: "acct-123", DisplayName: "A*[reviewer]_`name`"},
		Content:   bitbucket.Content{Raw: "Comment"},
		CreatedOn: time.Date(2026, time.March, 10, 14, 3, 0, 0, time.UTC),
		Inline:    &bitbucket.Inline{Path: "data_query/app.py", To: intPtr(411)},
	}}

	if err := writer.WriteComments(comments, false); err != nil {
		t.Fatalf("WriteComments() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Author: @A\\*\\[reviewer\\]\\_\\`name\\` (author)") {
		t.Fatalf("output missing escaped author:\n%s", output)
	}
}

func TestCommentWriterResolvesMentionsAndStripsZeroWidthCharacters(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewCommentWriter(&buf, bitbucket.User{})
	writer.SetUserResolver(func(accountID string) (bitbucket.User, bool) {
		if accountID == "acct-123" {
			return bitbucket.User{DisplayName: "Hiep Nguyen"}, true
		}
		return bitbucket.User{}, false
	})

	comments := []bitbucket.Comment{{
		ID:        1,
		User:      bitbucket.User{DisplayName: "Reviewer"},
		Content:   bitbucket.Content{Raw: "Please check @{acct-123}\n\u200c"},
		CreatedOn: time.Date(2026, time.March, 10, 14, 3, 0, 0, time.UTC),
	}}

	if err := writer.WriteComments(comments, false); err != nil {
		t.Fatalf("WriteComments() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Please check @Hiep Nguyen") {
		t.Fatalf("output missing resolved mention:\n%s", output)
	}
	if strings.Contains(output, "\u200c") {
		t.Fatalf("output still contains zero-width character:\n%q", output)
	}
}

func intPtr(value int) *int {
	return &value
}
