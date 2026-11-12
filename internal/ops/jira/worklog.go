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
			"skipped, so retrying a batch is safe. Returns a status per entry.",
		Surfaces: ops.Both,
	},
	Run: worklogAdd,
}

type worklogPlan struct {
	key     string
	started time.Time
	seconds int
	comment json.RawMessage
}

func (p worklogPlan) dedupKey() string {
	return fmt.Sprintf("%s|%d|%d", p.key, p.started.Unix(), p.seconds)
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
	logged, listErrs := existingWorklogs(ctx, client, me.AccountID, plans)
	result := &WorklogAddResult{Results: make([]WorklogEntryResult, len(plans))}
	failed := 0
	for i, plan := range plans {
		r := &result.Results[i]
		*r = WorklogEntryResult{Index: i, Issue: plan.key, Started: jiraapi.FormatTime(plan.started), Duration: formatSeconds(plan.seconds)}
		if err := listErrs[plan.key]; err != nil {
			r.Status, r.Error = StatusFailed, "could not check for an existing worklog: "+err.Error()
			failed++
			continue
		}
		if id, ok := logged[plan.dedupKey()]; ok {
			r.Status, r.WorklogID = StatusSkipped, id
			continue
		}
		w, err := client.AddWorklog(ctx, plan.key, jiraapi.WorklogInput{
			Started:          jiraapi.FormatTime(plan.started),
			TimeSpentSeconds: plan.seconds,
			Comment:          plan.comment,
		}, jiraapi.WorklogAddOptions{})
		if err != nil {
			r.Status, r.Error = StatusFailed, err.Error()
			failed++
			continue
		}
		r.Status, r.WorklogID = StatusOK, w.ID
		logged[plan.dedupKey()] = w.ID
	}
	return result, ops.PartialOf(failed, len(plans), "worklog entries")
}

func planWorklog(entry WorklogEntry) (worklogPlan, error) {
	key, err := issueKey("issue", entry.Issue)
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
	plan := worklogPlan{key: key, started: started, seconds: seconds}
	if strings.TrimSpace(entry.Comment) != "" {
		if plan.comment, err = markdownBody("comment", entry.Comment); err != nil {
			return worklogPlan{}, err
		}
	}
	return plan, nil
}

// existingWorklogs maps the dedup key of each worklog the caller already
// logged in the batch's time range to its ID, per issue. An issue whose
// worklogs could not be listed gets an error instead, and its entries fail
// rather than risk logging twice.
func existingWorklogs(ctx context.Context, client *jiraapi.Client, accountID string, plans []worklogPlan) (map[string]string, map[string]error) {
	type span struct{ from, to time.Time }
	spans := map[string]*span{}
	var order []string
	for _, plan := range plans {
		s, ok := spans[plan.key]
		if !ok {
			spans[plan.key] = &span{plan.started, plan.started}
			order = append(order, plan.key)
			continue
		}
		if plan.started.Before(s.from) {
			s.from = plan.started
		}
		if plan.started.After(s.to) {
			s.to = plan.started
		}
	}
	logged := map[string]string{}
	errs := map[string]error{}
	for _, key := range order {
		s := spans[key]
		worklogs, total, err := client.WorklogsUpTo(ctx, key, jiraapi.WorklogListOptions{
			StartedAfter:  s.from.Add(-time.Minute).UnixMilli(),
			StartedBefore: s.to.Add(time.Minute).UnixMilli(),
		}, MaxWorklogs)
		if err != nil {
			errs[key] = err
			continue
		}
		if total > len(worklogs) {
			errs[key] = fmt.Errorf("%s has %d worklogs in the batch's time range, more than the %d atlas checks", key, total, MaxWorklogs)
			continue
		}
		for _, w := range worklogs {
			if w.Author == nil || w.Author.AccountID != accountID {
				continue
			}
			started, err := jiraapi.ParseTime(w.Started)
			if err != nil {
				continue
			}
			plan := worklogPlan{key: key, started: started, seconds: w.TimeSpentSeconds}
			logged[plan.dedupKey()] = w.ID
		}
	}
	return logged, errs
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
