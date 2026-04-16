package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestPRMarkdownWriterWritesFrontmatterAndReviewSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewPRMarkdownWriter(&buf)
	writer.SetContext(
		"moberg-analytics",
		"dashboard",
		[]bitbucket.Comment{
			{
				ID:        1,
				User:      bitbucket.User{DisplayName: "Justin Moore"},
				Content:   bitbucket.Content{Raw: "Reviewer comment"},
				CreatedOn: time.Date(2026, time.March, 10, 14, 3, 0, 0, time.UTC),
				Inline:    &bitbucket.Inline{Path: "data_query/app.py", To: intPtr(411)},
			},
			{
				ID:        2,
				User:      bitbucket.User{DisplayName: "Justin Moore"},
				Content:   bitbucket.Content{Raw: "General comment"},
				CreatedOn: time.Date(2026, time.March, 10, 14, 4, 0, 0, time.UTC),
			},
		},
		true,
		[]bitbucket.Task{
			{ID: 1, State: "OPEN"},
			{ID: 2, State: "RESOLVED"},
		},
		true,
	)
	pr := &bitbucket.PullRequest{
		ID:    757,
		Title: "Support showing an ECG channel in EEG montages",
		State: "OPEN",
		Author: bitbucket.User{
			Nickname:    "Tony Okeke",
			DisplayName: "Tony Okeke",
		},
		Source:      bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "feature/ecg"}},
		Destination: bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "testing"}},
		Reviewers:   []bitbucket.User{{UUID: "{reviewer-1}", Nickname: "Justin Moore", DisplayName: "Justin Moore"}},
		Participants: []bitbucket.Participant{{
			Role:  "REVIEWER",
			State: "changes_requested",
			User:  bitbucket.User{UUID: "{reviewer-1}", Nickname: "Justin Moore", DisplayName: "Justin Moore"},
		}},
		CommentCount: 2,
		TaskCount:    2,
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"---\npr: 757",
		"workspace: 'moberg-analytics'",
		"repo: 'dashboard'",
		"mention: '@Tony Okeke'",
		"# PR #757: Support showing an ECG channel in EEG montages",
		"## Review Summary",
		"- Reviewers: 1 changes requested",
		"- Comments: 2 total, 2 unresolved comments, 2 unresolved threads",
		"- Files with feedback: 1",
		"- Actionable: data_query/app.py:411 — @Justin Moore: Reviewer comment",
		"- Tasks: 1 open, 1 resolved",
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
	writer.SetContext(
		"moberg-analytics",
		"dashboard",
		[]bitbucket.Comment{{
			ID:        1,
			User:      bitbucket.User{DisplayName: "B*[reviewer]_`name`"},
			Content:   bitbucket.Content{Raw: "Comment body"},
			CreatedOn: time.Date(2026, time.March, 10, 14, 3, 0, 0, time.UTC),
		}},
		true,
		nil,
		false,
	)
	pr := &bitbucket.PullRequest{
		ID:          1,
		Title:       "Escape markdown names",
		State:       "OPEN",
		Author:      bitbucket.User{DisplayName: "A*[author]_`name`"},
		Source:      bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "feature"}},
		Destination: bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "main"}},
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"mention: '@A*[author]_`name`'",
		"- Actionable: general — @B\\*\\[reviewer\\]\\_\\`name\\`: Comment body",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}

func TestPRMarkdownWriterResolvesMentionsAndPreservesDescriptionWhitespace(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewPRMarkdownWriter(&buf)
	writer.SetUserResolver(func(accountID string) (bitbucket.User, bool) {
		if accountID == "acct-123" {
			return bitbucket.User{DisplayName: "Hiep Nguyen"}, true
		}
		return bitbucket.User{}, false
	})

	pr := &bitbucket.PullRequest{
		ID:          1,
		Title:       "Normalize description",
		State:       "OPEN",
		Description: "\nReview @{acct-123}\n\u200c\n",
		Author:      bitbucket.User{DisplayName: "Tony Okeke"},
		Source:      bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "feature"}},
		Destination: bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "main"}},
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Review @Hiep Nguyen") {
		t.Fatalf("output missing resolved mention:\n%s", output)
	}
	if strings.Contains(output, "\u200c") {
		t.Fatalf("output still contains zero-width character:\n%q", output)
	}
	if !strings.Contains(output, "## Description\n\n\nReview @Hiep Nguyen\n\n") {
		t.Fatalf("output did not preserve description whitespace:\n%q", output)
	}
}

func TestPRMarkdownWriterDoesNotResolveMentionsInsideCode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := NewPRMarkdownWriter(&buf)
	writer.SetUserResolver(func(accountID string) (bitbucket.User, bool) {
		if accountID == "acct-123" {
			return bitbucket.User{DisplayName: "Hiep Nguyen"}, true
		}
		return bitbucket.User{}, false
	})

	pr := &bitbucket.PullRequest{
		ID:          1,
		Title:       "Code mention preservation",
		State:       "OPEN",
		Description: "Plain @{acct-123}\n`inline @{acct-123}`\n```txt\nblock @{acct-123}\n```\n",
		Author:      bitbucket.User{DisplayName: "Tony Okeke"},
		Source:      bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "feature"}},
		Destination: bitbucket.PullRequestRef{Branch: bitbucket.Branch{Name: "main"}},
	}

	if err := writer.WritePR(pr); err != nil {
		t.Fatalf("WritePR() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Plain @Hiep Nguyen") {
		t.Fatalf("plain mention was not resolved:\n%s", output)
	}
	for _, expected := range []string{"`inline @{acct-123}`", "block @{acct-123}"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing code literal %q:\n%s", expected, output)
		}
	}
}
