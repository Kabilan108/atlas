package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

type PRMarkdownWriter struct {
	w              io.Writer
	resolveMention func(string) (bitbucket.User, bool)
}

func NewPRMarkdownWriter(w io.Writer) *PRMarkdownWriter {
	return &PRMarkdownWriter{w: w}
}

func (m *PRMarkdownWriter) SetUserResolver(resolveMention func(string) (bitbucket.User, bool)) {
	m.resolveMention = resolveMention
}

func (m *PRMarkdownWriter) WritePR(pr *bitbucket.PullRequest) error {
	fmt.Fprintf(m.w, "# PR #%d: %s\n\n", pr.ID, pr.Title)
	fmt.Fprintf(m.w, "**Author**: %s\n", formatUserMention(pr.Author))
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
		fmt.Fprintln(m.w, normalizeMarkdownText(pr.Description, m.resolveMention))
		fmt.Fprintln(m.w)
	}

	m.writeFooter(pr)
	return nil
}

func (m *PRMarkdownWriter) formatReviewers(pr *bitbucket.PullRequest) string {
	type reviewerEntry struct {
		label  string
		status string
	}

	reviewerMap := make(map[string]reviewerEntry)

	for _, r := range pr.Reviewers {
		key := r.IdentityKey()
		if key == "" {
			continue
		}
		reviewerMap[key] = reviewerEntry{
			label:  formatUserMention(r),
			status: "pending",
		}
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
		key := p.User.IdentityKey()
		if key == "" {
			continue
		}
		reviewerMap[key] = reviewerEntry{
			label:  formatUserMention(p.User),
			status: status,
		}
	}

	if len(reviewerMap) == 0 {
		return ""
	}

	entries := make([]reviewerEntry, 0, len(reviewerMap))
	for _, entry := range reviewerMap {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].label < entries[j].label
	})

	var parts []string
	for _, entry := range entries {
		parts = append(parts, fmt.Sprintf("%s (%s)", entry.label, entry.status))
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
