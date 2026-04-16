package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestPRMarkdownWriterUsesUserFallbacks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewPRMarkdownWriter(&buf)
	pr := &bitbucket.PullRequest{
		ID:    757,
		Title: "Support showing an ECG channel in EEG montages",
		State: "OPEN",
		Author: bitbucket.User{
			Nickname:    "Tony Okeke",
			DisplayName: "Tony Okeke",
		},
		Source: bitbucket.PullRequestRef{
			Branch: bitbucket.Branch{Name: "feature/ecg"},
		},
		Destination: bitbucket.PullRequestRef{
			Branch: bitbucket.Branch{Name: "testing"},
		},
		Reviewers: []bitbucket.User{
			{UUID: "{reviewer-1}", Nickname: "Justin Moore", DisplayName: "Justin Moore"},
		},
		Participants: []bitbucket.Participant{
			{
				Role:  "REVIEWER",
				State: "changes_requested",
				User: bitbucket.User{
					UUID:        "{reviewer-1}",
					Nickname:    "Justin Moore",
					DisplayName: "Justin Moore",
				},
			},
		},
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"**Author**: @Tony Okeke",
		"**Reviewers**: @Justin Moore (changes_requested)",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}

func TestPRMarkdownWriterEscapesMarkdownNames(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewPRMarkdownWriter(&buf)
	pr := &bitbucket.PullRequest{
		ID:    1,
		Title: "Escape markdown names",
		State: "OPEN",
		Author: bitbucket.User{
			DisplayName: "A*[author]_`name`",
		},
		Source: bitbucket.PullRequestRef{
			Branch: bitbucket.Branch{Name: "feature"},
		},
		Destination: bitbucket.PullRequestRef{
			Branch: bitbucket.Branch{Name: "main"},
		},
		Reviewers: []bitbucket.User{
			{UUID: "{reviewer-1}", DisplayName: "B*[reviewer]_`name`"},
		},
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"**Author**: @A\\*\\[author\\]\\_\\`name\\`",
		"**Reviewers**: @B\\*\\[reviewer\\]\\_\\`name\\` (pending)",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}
