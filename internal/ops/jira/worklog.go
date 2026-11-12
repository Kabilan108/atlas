package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/jira/adf"
	"github.com/kabilan108/atlas/internal/ops"
)

// WorklogsResult is an issue's worklogs, up to MaxWorklogs, and how many
// it has in all.
type WorklogsResult struct {
	Key      string            `json:"key"`
	Worklogs []jiraapi.Worklog `json:"worklogs"`
	Total    int               `json:"total"`
}

var WorklogList = &ops.Op[IssueRef, *WorklogsResult]{
	Meta: ops.Meta{
		Name:        "jira_worklog_list",
		Description: "List a Jira issue's worklogs (all pages, up to 1000): start, author, time spent, ID and comment, with the total time.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run: func(ctx context.Context, env ops.Env, in IssueRef) (*WorklogsResult, error) {
		key, err := in.Key()
		if err != nil {
			return nil, err
		}
		client, err := env.Jira(ctx)
		if err != nil {
			return nil, err
		}
		worklogs, total, err := client.WorklogsUpTo(ctx, key, jiraapi.WorklogListOptions{}, MaxWorklogs)
		if err != nil {
			return nil, err
		}
		return &WorklogsResult{Key: key, Worklogs: worklogs, Total: total}, nil
	},
	Markdown: (*WorklogsResult).Markdown,
}

func (r *WorklogsResult) Markdown() (string, error) {
	if len(r.Worklogs) == 0 {
		return fmt.Sprintf("No worklogs on %s.\n", r.Key), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Worklogs on %s:\n\n| Started | Author | Time | ID | Comment |\n| --- | --- | --- | --- | --- |\n", r.Key)
	total := 0
	for _, w := range r.Worklogs {
		total += w.TimeSpentSeconds
		comment, err := adf.ToMarkdown(w.Comment)
		if err != nil {
			return "", fmt.Errorf("render worklog %s comment: %w", w.ID, err)
		}
		spent := w.TimeSpent
		if spent == "" {
			spent = formatSeconds(w.TimeSpentSeconds)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", when(w.Started), cell(userName(w.Author)), spent, w.ID, cell(comment))
	}
	noun := "worklogs"
	if len(r.Worklogs) == 1 {
		noun = "worklog"
	}
	if r.Total > len(r.Worklogs) {
		fmt.Fprintf(&b, "\nFirst %d of %d worklogs, %s in those shown.\n", len(r.Worklogs), r.Total, formatSeconds(total))
		return b.String(), nil
	}
	fmt.Fprintf(&b, "\n%d %s, %s total.\n", len(r.Worklogs), noun, formatSeconds(total))
	return b.String(), nil
}

// WorklogEntry is one worklog to add.
type WorklogEntry struct {
	Issue    string `json:"issue" jsonschema:"issue key"`
	Started  string `json:"started" jsonschema:"ISO 8601 start with offset, e.g. 2026-11-11T09:30:00-05:00"`
	Duration string `json:"duration" jsonschema:"time spent: 1h 30m, 45m, 1.5h or PT1H30M"`
	Comment  string `json:"comment,omitempty" jsonschema:"markdown"`
}

// WorklogAddInput is the input to jira_worklog_add.
type WorklogAddInput struct {
	Entries []WorklogEntry `json:"entries" jsonschema:"up to 200"`
}

// Batch entry statuses.
const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// WorklogEntryResult says what happened to one entry.
type WorklogEntryResult struct {
	Index     int    `json:"index"`
	Status    string `json:"status"`
	Issue     string `json:"issue"`
	Started   string `json:"started"`
	Duration  string `json:"duration"`
	WorklogID string `json:"worklog_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WorklogAddResult has one result per entry, in input order.
type WorklogAddResult struct {
	Results []WorklogEntryResult `json:"results"`
}

var WorklogAdd = &ops.Op[WorklogAddInput, *WorklogAddResult]{
	Meta: ops.Meta{
		Name: "jira_worklog_add",
		Description: "Log work on Jira issues in one batch. An entry you already logged (same issue, start and duration) is " +
			"skipped, so retrying through the same atlas server is safe; concurrent calls via different servers can still " +
			"log twice. Returns a status per entry.",
		Surfaces: ops.Both,
	},
	Run: worklogAdd,
}

type worklogPlan struct {
	ref     string
	started time.Time
	seconds int
	comment json.RawMessage
}

// worklogIdentity is what makes two worklogs the same entry: the issue's
// immutable ID (keys change when an issue moves), the start as Jira stores
// it (to the millisecond, as an instant), and the time spent in seconds.
func worklogIdentity(issueID string, started time.Time, seconds int) string {
	return fmt.Sprintf("%s|%d|%d", issueID, started.UnixMilli(), seconds)
}

type worklogIssue struct {
	id, key string
	err     error
}

func worklogAdd(ctx context.Context, env ops.Env, in WorklogAddInput) (*WorklogAddResult, error) {
	if len(in.Entries) == 0 || len(in.Entries) > MaxWorklogEntries {
		return nil, ops.Errorf(ops.Invalid, "entries must hold 1 to %d worklogs", MaxWorklogEntries)
	}
	plans := make([]worklogPlan, len(in.Entries))
	for i, entry := range in.Entries {
		plan, err := planWorklog(entry)
		if err != nil {
			return nil, ops.Errorf(ops.Invalid, "entries[%d]: %w", i, err)
		}
		plans[i] = plan
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	me, err := client.Myself(ctx)
	if err != nil {
		return nil, fmt.Errorf("find your account: %w", err)
	}
	issues := map[string]worklogIssue{}
	for _, plan := range plans {
		if _, ok := issues[plan.ref]; !ok {
			issues[plan.ref] = findWorklogIssue(ctx, client, plan.ref)
		}
	}
	logger := &worklogger{client: client, accountID: me.AccountID, logged: map[string]string{}}
	result := &WorklogAddResult{Results: make([]WorklogEntryResult, len(plans))}
	failed := 0
	for i, plan := range plans {
		issue := issues[plan.ref]
		r := &result.Results[i]
		*r = WorklogEntryResult{Index: i, Issue: plan.ref, Started: jiraapi.FormatTime(plan.started), Duration: formatSeconds(plan.seconds)}
		if issue.err != nil {
			r.Status, r.Error = StatusFailed, "could not find the issue: "+issue.err.Error()
			failed++
			continue
		}
		r.Issue = issue.key
		status, id, err := logger.add(ctx, issue.id, plan)
		r.Status, r.WorklogID = status, id
		if err != nil {
			r.Error = err.Error()
			failed++
		}
	}
	return result, ops.PartialOf(failed, len(plans), "worklog entries")
}

// findWorklogIssue reads an issue's ID and current key from its key or ID.
func findWorklogIssue(ctx context.Context, client *jiraapi.Client, ref string) worklogIssue {
	issue, err := client.GetIssue(ctx, ref, jiraapi.GetIssueOptions{Fields: []string{"summary"}})
	if err != nil {
		return worklogIssue{err: err}
	}
	if !issueIDPattern.MatchString(issue.ID) {
		return worklogIssue{err: fmt.Errorf("jira returned no numeric ID for %s", ref)}
	}
	key := issue.Key
	if key == "" {
		key = ref
	}
	return worklogIssue{id: issue.ID, key: key}
}

// worklogLocks serializes the check and the POST for one site, account and
// issue across every call in this process.
var worklogLocks keyedLocks

// worklogger adds one batch's worklogs, skipping entries already logged.
type worklogger struct {
	client    *jiraapi.Client
	accountID string
	// logged maps worklogIdentity to the worklog ID for this batch's
	// entries, so a repeated entry is skipped even before Jira lists it.
	logged map[string]string
}

// add logs plan on issueID unless the caller already logged it. Under the
// issue's lock it lists the worklogs around plan's start right before the
// POST, so calls in this process never both see none and both log it.
// Calls from other processes can still race.
func (l *worklogger) add(ctx context.Context, issueID string, plan worklogPlan) (status, id string, err error) {
	identity := worklogIdentity(issueID, plan.started, plan.seconds)
	if id, ok := l.logged[identity]; ok {
		return StatusSkipped, id, nil
	}
	unlock, err := worklogLocks.lock(ctx, l.client.BaseURL()+"\x00"+l.accountID+"\x00"+issueID)
	if err != nil {
		return StatusFailed, "", err
	}
	defer unlock()
	existing, err := l.existing(ctx, issueID, plan)
	if err != nil {
		return StatusFailed, "", fmt.Errorf("could not check for an existing worklog: %w", err)
	}
	if existing != "" {
		l.logged[identity] = existing
		return StatusSkipped, existing, nil
	}
	w, err := l.client.AddWorklog(ctx, issueID, jiraapi.WorklogInput{
		Started:          jiraapi.FormatTime(plan.started),
		TimeSpentSeconds: plan.seconds,
		Comment:          plan.comment,
	}, jiraapi.WorklogAddOptions{})
	if err != nil {
		return StatusFailed, "", err
	}
	l.logged[identity] = w.ID
	return StatusOK, w.ID, nil
}

// existing returns the ID of the caller's worklog matching plan on issueID,
// or "" when there is none.
func (l *worklogger) existing(ctx context.Context, issueID string, plan worklogPlan) (string, error) {
	worklogs, total, err := l.client.WorklogsUpTo(ctx, issueID, jiraapi.WorklogListOptions{
		StartedAfter:  plan.started.Add(-time.Minute).UnixMilli(),
		StartedBefore: plan.started.Add(time.Minute).UnixMilli(),
	}, MaxWorklogs)
	if err != nil {
		return "", err
	}
	if total > len(worklogs) {
		return "", fmt.Errorf("%d worklogs start within a minute of %s, more than the %d atlas checks", total, jiraapi.FormatTime(plan.started), MaxWorklogs)
	}
	want := worklogIdentity(issueID, plan.started, plan.seconds)
	for _, w := range worklogs {
		if w.Author == nil || w.Author.AccountID != l.accountID {
			continue
		}
		started, err := jiraapi.ParseTime(w.Started)
		if err != nil {
			continue
		}
		if worklogIdentity(issueID, started, w.TimeSpentSeconds) == want {
			return w.ID, nil
		}
	}
	return "", nil
}

func planWorklog(entry WorklogEntry) (worklogPlan, error) {
	ref, err := issueKey("issue", entry.Issue)
	if err != nil {
		return worklogPlan{}, err
	}
	started, err := jiraapi.ParseTime(entry.Started)
	if err != nil {
		return worklogPlan{}, fmt.Errorf("started: %w", err)
	}
	seconds, err := parseDuration(entry.Duration)
	if err != nil {
		return worklogPlan{}, err
	}
	plan := worklogPlan{ref: ref, started: started, seconds: seconds}
	if strings.TrimSpace(entry.Comment) != "" {
		if plan.comment, err = markdownBody("comment", entry.Comment); err != nil {
			return worklogPlan{}, err
		}
	}
	return plan, nil
}

var durationPart = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([a-z]+)`)

// parseDuration reads 1h 30m, 1h30m, 45m, 1.5h or ISO 8601 PT1H30M as
// seconds. Days and weeks are refused: their length is a site setting.
func parseDuration(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if rest, ok := strings.CutPrefix(strings.ToUpper(s), "PT"); ok {
		s = rest
	}
	if s == "" {
		return 0, fmt.Errorf("duration is required, e.g. 1h 30m")
	}
	total := 0.0
	for s != "" {
		m := durationPart.FindStringSubmatch(s)
		if m == nil {
			return 0, fmt.Errorf("duration %q: use hours and minutes, e.g. 1h 30m or PT1H30M", raw)
		}
		n, _ := strconv.ParseFloat(m[1], 64)
		switch strings.ToLower(m[2]) {
		case "h", "hr", "hrs", "hour", "hours":
			total += n * 3600
		case "m", "min", "mins", "minute", "minutes":
			total += n * 60
		case "s", "sec", "secs":
			total += n
		case "d", "w", "day", "days", "week", "weeks":
			return 0, fmt.Errorf("duration %q: days and weeks depend on Jira's settings; give hours, e.g. 8h", raw)
		default:
			return 0, fmt.Errorf("duration %q: unknown unit %q; use h and m", raw, m[2])
		}
		s = strings.TrimSpace(s[len(m[0]):])
	}
	seconds := int(math.Round(total))
	if seconds < 60 {
		return 0, fmt.Errorf("duration %q: Jira logs at least one minute", raw)
	}
	return seconds, nil
}
