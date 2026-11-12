// Package repo holds read-only Bitbucket repository operations: repositories,
// branches, commits, files, and workspace users.
package repo

import (
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// Ops returns the repository ops in a stable order.
func Ops() []ops.Def {
	return []ops.Def{
		List,
		BranchList,
		BranchView,
		CommitList,
		FileRead,
		UserSearch,
		Whoami,
	}
}

// Every list has a default and a hard maximum, applied before the client
// is asked for one more to learn whether there are more.
const (
	maxRepoLimit   = 1000
	maxBranchLimit = 1000
	maxCommitLimit = 1000
	maxUserLimit   = 100
	// maxMembersScanned bounds the members user_search reads while matching
	// names client-side.
	maxMembersScanned = 2000
)

// limitOrDefault applies a list limit's default and rejects values outside
// 1..maximum.
func limitOrDefault(limit, fallback, maximum int) (int, error) {
	switch {
	case limit == 0:
		return fallback, nil
	case limit < 0:
		return 0, ops.Errorf(ops.Invalid, "limit must be greater than 0")
	case limit > maximum:
		return 0, ops.Errorf(ops.Invalid, "limit %d is over the maximum of %d", limit, maximum)
	}
	return limit, nil
}

// trimMore cuts values fetched with limit+1 back to limit and reports
// whether there were more.
func trimMore[T any](values []T, limit int) ([]T, bool) {
	if len(values) > limit {
		return values[:limit], true
	}
	return values, false
}

func moreNote(more bool, limit int) string {
	if !more {
		return ""
	}
	return "\nShowing the first " + strconv.Itoa(limit) + "; raise limit or narrow the filter for more.\n"
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return strings.TrimSpace(line)
}

func authorName(author bitbucket.CommitAuthor) string {
	if author.User != nil && author.User.DisplayName != "" {
		return author.User.DisplayName
	}
	if name, _, ok := strings.Cut(author.Raw, " <"); ok {
		return name
	}
	return author.Raw
}
