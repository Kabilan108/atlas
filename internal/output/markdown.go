package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

type PRMarkdownWriter struct {
	w              io.Writer
	resolveMention func(string) (bitbucket.User, bool)
	workspace      string
	repo           string
	comments       []bitbucket.Comment
	commentsLoaded bool
	tasks          []bitbucket.Task
	tasksLoaded    bool
}

type reviewerEntry struct {
	name       string
	label      string
	rawMention string
	status     string
}

func NewPRMarkdownWriter(w io.Writer) *PRMarkdownWriter {
	return &PRMarkdownWriter{w: w}
}

func (m *PRMarkdownWriter) SetUserResolver(resolveMention func(string) (bitbucket.User, bool)) {
	m.resolveMention = resolveMention
}

func (m *PRMarkdownWriter) SetContext(workspace, repo string, comments []bitbucket.Comment, commentsLoaded bool, tasks []bitbucket.Task, tasksLoaded bool) {
	m.workspace = workspace
	m.repo = repo
	m.comments = comments
	m.commentsLoaded = commentsLoaded
	m.tasks = tasks
	m.tasksLoaded = tasksLoaded
}

func (m *PRMarkdownWriter) WritePR(pr *bitbucket.PullRequest) error {
	m.writeFrontmatter(pr)
	fmt.Fprintf(m.w, "# PR #%d: %s\n\n", pr.ID, pr.Title)
	m.writeReviewSummary(pr)

	if pr.Description != "" {
		fmt.Fprintln(m.w, "## Description")
		fmt.Fprintln(m.w)
		fmt.Fprintln(m.w, normalizeMarkdownText(pr.Description, m.resolveMention))
		fmt.Fprintln(m.w)
	}

	return nil
}

func (m *PRMarkdownWriter) reviewerEntries(pr *bitbucket.PullRequest) []reviewerEntry {
	reviewerMap := make(map[string]reviewerEntry)

	for _, reviewer := range pr.Reviewers {
		key := reviewer.IdentityKey()
		if key == "" {
			continue
		}
		reviewerMap[key] = reviewerEntry{
			name:       reviewer.Handle(),
			label:      formatUserMention(reviewer),
			rawMention: rawUserMention(reviewer),
			status:     "pending",
		}
	}

	for _, participant := range pr.Participants {
		if participant.Role != "REVIEWER" {
			continue
		}

		status := "pending"
		if participant.Approved {
			status = "approved"
		} else if participant.State == "changes_requested" {
			status = "changes_requested"
		}

		key := participant.User.IdentityKey()
		if key == "" {
			continue
		}
		reviewerMap[key] = reviewerEntry{
			name:       participant.User.Handle(),
			label:      formatUserMention(participant.User),
			rawMention: rawUserMention(participant.User),
			status:     status,
		}
	}

	entries := make([]reviewerEntry, 0, len(reviewerMap))
	for _, reviewer := range reviewerMap {
		entries = append(entries, reviewer)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].label < entries[j].label
	})

	return entries
}

func (m *PRMarkdownWriter) writeFrontmatter(pr *bitbucket.PullRequest) {
	reviewers := m.reviewerEntries(pr)

	fmt.Fprintln(m.w, "---")
	fmt.Fprintf(m.w, "pr: %d\n", pr.ID)
	if m.workspace != "" {
		fmt.Fprintf(m.w, "workspace: %s\n", yamlString(m.workspace))
	}
	if m.repo != "" {
		fmt.Fprintf(m.w, "repo: %s\n", yamlString(m.repo))
	}
	fmt.Fprintf(m.w, "title: %s\n", yamlString(pr.Title))
	fmt.Fprintf(m.w, "state: %s\n", yamlString(pr.State))
	fmt.Fprintln(m.w, "author:")
	fmt.Fprintf(m.w, "  name: %s\n", yamlString(pr.Author.Handle()))
	fmt.Fprintf(m.w, "  mention: %s\n", yamlString(rawUserMention(pr.Author)))
	fmt.Fprintf(m.w, "source_branch: %s\n", yamlString(pr.Source.Branch.Name))
	fmt.Fprintf(m.w, "destination_branch: %s\n", yamlString(pr.Destination.Branch.Name))
	fmt.Fprintf(m.w, "updated_at: %s\n", yamlString(pr.UpdatedOn.UTC().Format(time.RFC3339)))
	fmt.Fprintf(m.w, "bitbucket_url: %s\n", yamlString(pr.Links.HTML.Href))
	fmt.Fprintf(m.w, "comment_count: %d\n", pr.CommentCount)
	if m.commentsLoaded {
		fmt.Fprintf(m.w, "unresolved_comment_count: %d\n", countUnresolvedComments(m.comments))
	}
	fmt.Fprintf(m.w, "task_count: %d\n", pr.TaskCount)

	if len(reviewers) == 0 {
		fmt.Fprintln(m.w, "reviewers: []")
	} else {
		fmt.Fprintln(m.w, "reviewers:")
		for _, reviewer := range reviewers {
			fmt.Fprintln(m.w, "  -")
			fmt.Fprintf(m.w, "    name: %s\n", yamlString(reviewer.name))
			fmt.Fprintf(m.w, "    mention: %s\n", yamlString(reviewer.rawMention))
			fmt.Fprintf(m.w, "    status: %s\n", yamlString(reviewer.status))
		}
	}

	fmt.Fprintln(m.w, "---")
	fmt.Fprintln(m.w)
}

func (m *PRMarkdownWriter) writeReviewSummary(pr *bitbucket.PullRequest) {
	reviewers := m.reviewerEntries(pr)
	if len(reviewers) == 0 && pr.CommentCount == 0 && pr.TaskCount == 0 && !(m.commentsLoaded && len(m.comments) > 0) && !(m.tasksLoaded && len(m.tasks) > 0) {
		return
	}

	fmt.Fprintln(m.w, "## Review Summary")
	fmt.Fprintln(m.w)

	if len(reviewers) > 0 {
		fmt.Fprintf(m.w, "- Reviewers: %s\n", formatReviewerSummary(reviewers))
	}

	if m.commentsLoaded {
		items := unresolvedThreadSummaries(m.comments, m.resolveMention)
		fmt.Fprintf(m.w, "- Comments: %d total, %d unresolved comments, %d unresolved threads\n", pr.CommentCount, countUnresolvedComments(m.comments), len(items))
		fmt.Fprintf(m.w, "- Files with feedback: %d\n", countFilesWithFeedback(items))
		for _, item := range items {
			fmt.Fprintf(m.w, "- Actionable: %s\n", item)
		}
	} else if pr.CommentCount > 0 {
		fmt.Fprintf(m.w, "- Comments: %d total\n", pr.CommentCount)
	}

	if m.tasksLoaded {
		openTasks, resolvedTasks := summarizeTasks(m.tasks)
		if openTasks > 0 || resolvedTasks > 0 {
			fmt.Fprintf(m.w, "- Tasks: %d open, %d resolved\n", openTasks, resolvedTasks)
		}
	} else if pr.TaskCount > 0 {
		fmt.Fprintf(m.w, "- Tasks: %d total\n", pr.TaskCount)
	}

	fmt.Fprintln(m.w)
}

func formatReviewerSummary(reviewers []reviewerEntry) string {
	counts := map[string]int{
		"approved":          0,
		"changes_requested": 0,
		"pending":           0,
	}

	for _, reviewer := range reviewers {
		counts[reviewer.status]++
	}

	var parts []string
	for _, key := range []string{"approved", "changes_requested", "pending"} {
		if counts[key] == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", counts[key], strings.ReplaceAll(key, "_", " ")))
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

func yamlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func countUnresolvedComments(comments []bitbucket.Comment) int {
	count := 0
	for _, comment := range comments {
		if comment.Deleted || comment.IsResolved() {
			continue
		}
		count++
	}
	return count
}

func unresolvedThreadSummaries(comments []bitbucket.Comment, resolveMention func(string) (bitbucket.User, bool)) []string {
	var summaries []string
	for _, comment := range comments {
		if comment.Deleted || comment.Parent != nil || comment.IsResolved() {
			continue
		}

		location := "general"
		if comment.Inline != nil {
			line := 0
			if comment.Inline.To != nil {
				line = *comment.Inline.To
			} else if comment.Inline.From != nil {
				line = *comment.Inline.From
			}
			if line > 0 {
				location = fmt.Sprintf("%s:%d", comment.Inline.Path, line)
			} else if comment.Inline.Path != "" {
				location = comment.Inline.Path
			}
		}

		body := summarizeCommentBody(normalizeMarkdownText(comment.Content.Raw, resolveMention))
		summaries = append(summaries, fmt.Sprintf("%s — %s: %s", location, formatUserMention(comment.User), body))
	}

	sort.Strings(summaries)
	return summaries
}

func summarizeCommentBody(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, ". "); idx >= 0 {
		text = text[:idx+1]
	}
	if len(text) > 120 {
		return text[:117] + "..."
	}
	return text
}

func countFilesWithFeedback(items []string) int {
	files := make(map[string]struct{})
	for _, item := range items {
		location, _, found := strings.Cut(item, " — ")
		if !found {
			continue
		}
		file, _, ok := strings.Cut(location, ":")
		if !ok {
			continue
		}
		files[file] = struct{}{}
	}
	return len(files)
}

func summarizeTasks(tasks []bitbucket.Task) (open int, resolved int) {
	for _, task := range tasks {
		if task.IsResolved() {
			resolved++
			continue
		}
		open++
	}
	return open, resolved
}
