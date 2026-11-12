package jira

import "github.com/kabilan108/atlas/internal/ops"

// Every count a caller can raise has a hard maximum, applied while paging,
// so no call reads an unbounded amount from Jira. The CLI maps "all" (-1)
// to these maxima.
const (
	MaxViewComments   = 100
	MaxHistory        = 500
	MaxSearchResults  = 1000
	MaxProjects       = 1000
	MaxWorklogs       = 1000
	MaxWorklogEntries = 200
	MaxAttachFiles    = 20
	// MaxAttachBytes caps the decoded files one jira_attach call uploads.
	MaxAttachBytes = 10 << 20
	// MaxBodyBytes caps a markdown body (description, comment, worklog
	// comment).
	MaxBodyBytes = 128 << 10
)

// boundedCount applies a default to zero and rejects counts outside
// 1..maximum.
func boundedCount(field string, n, def, maximum int) (int, error) {
	switch {
	case n == 0:
		return def, nil
	case n < 0 || n > maximum:
		return 0, ops.Errorf(ops.Invalid, "%s must be between 1 and %d", field, maximum)
	}
	return n, nil
}
