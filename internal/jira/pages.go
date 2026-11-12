package jira

import (
	"context"
	"errors"
	"fmt"
)

// The UpTo and Newest variants stop paging at limit items, so a caller never
// holds more than it asked for however many the issue has.

var errLimit = errors.New("jira: limit must be positive")

// WorklogsUpTo returns an issue's first limit worklogs and the number Jira
// reports in all.
func (c *Client) WorklogsUpTo(ctx context.Context, issueKey string, opts WorklogListOptions, limit int) ([]Worklog, int, error) {
	if limit <= 0 {
		return nil, 0, errLimit
	}
	var all []Worklog
	total := 0
	for startAt := 0; len(all) < limit; {
		q := pageQuery(startAt, min(1000, limit-len(all)))
		if opts.StartedAfter > 0 {
			q.Set("startedAfter", fmt.Sprint(opts.StartedAfter))
		}
		if opts.StartedBefore > 0 {
			q.Set("startedBefore", fmt.Sprint(opts.StartedBefore))
		}
		var page struct {
			StartAt  int       `json:"startAt"`
			Total    int       `json:"total"`
			Worklogs []Worklog `json:"worklogs"`
		}
		if err := c.getJSON(ctx, issuePath(issueKey, "worklog"), q, &page); err != nil {
			return nil, 0, err
		}
		total = page.Total
		all = append(all, page.Worklogs...)
		startAt = page.StartAt + len(page.Worklogs)
		if len(page.Worklogs) == 0 || startAt >= total {
			break
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, max(total, len(all)), nil
}

// ChangelogNewest returns an issue's newest limit changelog entries, oldest
// first, and the number Jira reports in all.
func (c *Client) ChangelogNewest(ctx context.Context, issueKey string, limit int) ([]ChangelogHistory, int, error) {
	if limit <= 0 {
		return nil, 0, errLimit
	}
	fetch := func(startAt, size int) ([]ChangelogHistory, int, error) {
		var page struct {
			Total  int                `json:"total"`
			Values []ChangelogHistory `json:"values"`
		}
		err := c.getJSON(ctx, issuePath(issueKey, "changelog"), pageQuery(startAt, size), &page)
		return page.Values, page.Total, err
	}
	first, total, err := fetch(0, min(defaultPageSize, limit))
	if err != nil {
		return nil, 0, err
	}
	var all []ChangelogHistory
	startAt := 0
	if total <= limit {
		all, startAt = first, len(first)
	} else {
		startAt = total - limit
	}
	for len(all) < limit && startAt < total {
		values, _, err := fetch(startAt, min(defaultPageSize, limit-len(all)))
		if err != nil {
			return nil, 0, err
		}
		if len(values) == 0 {
			break
		}
		all = append(all, values...)
		startAt += len(values)
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, max(total, len(all)), nil
}
