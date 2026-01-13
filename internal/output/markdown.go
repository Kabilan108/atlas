package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

type PRMarkdownWriter struct {
	w io.Writer
}

func NewPRMarkdownWriter(w io.Writer) *PRMarkdownWriter {
	return &PRMarkdownWriter{w: w}
}

func (m *PRMarkdownWriter) WritePR(pr *bitbucket.PullRequest) error {
	fmt.Fprintf(m.w, "# PR #%d: %s\n\n", pr.ID, pr.Title)
	fmt.Fprintf(m.w, "**Author**: @%s\n", pr.Author.Username)
	fmt.Fprintf(m.w, "**State**: %s\n", pr.State)
	fmt.Fprintf(m.w, "**Branch**: %s → %s\n", pr.Source.Branch.Name, pr.Destination.Branch.Name)

	reviewerStatus := m.formatReviewers(pr)
	if reviewerStatus != "" {
		fmt.Fprintf(m.w, "**Reviewers**: %s\n", reviewerStatus)
	}

	fmt.Fprintln(m.w)

	if pr.Description != "" {
		fmt.Fprintln(m.w, "## Description")
		fmt.Fprintln(m.w)
		fmt.Fprintln(m.w, pr.Description)
		fmt.Fprintln(m.w)
	}

	m.writeFooter(pr)
	return nil
}

func (m *PRMarkdownWriter) formatReviewers(pr *bitbucket.PullRequest) string {
	reviewerMap := make(map[string]string)

	for _, r := range pr.Reviewers {
		reviewerMap[r.Username] = "pending"
	}

	for _, p := range pr.Participants {
		if p.Role != "REVIEWER" {
			continue
		}
		status := "pending"
		if p.Approved {
			status = "approved"
		} else if p.State == "changes_requested" {
			status = "changes_requested"
		}
		reviewerMap[p.User.Username] = status
	}

	if len(reviewerMap) == 0 {
		return ""
	}

	var parts []string
	for username, status := range reviewerMap {
		parts = append(parts, fmt.Sprintf("@%s (%s)", username, status))
	}

	return strings.Join(parts, ", ")
}

func (m *PRMarkdownWriter) writeFooter(pr *bitbucket.PullRequest) {
	var parts []string
	if pr.CommentCount > 0 {
		parts = append(parts, fmt.Sprintf("%d comments", pr.CommentCount))
	}
	if pr.TaskCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tasks", pr.TaskCount))
	}
	if len(parts) > 0 {
		fmt.Fprintln(m.w, strings.Join(parts, ", "))
	}
}
