package pr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// ActivityLimit is how many of the newest activity entries pr_view shows.
const ActivityLimit = 50

// writeActivity appends the feed, newest first, one line per entry.
func writeActivity(buf *bytes.Buffer, feed []bitbucket.PullRequestActivity) {
	if len(feed) == 0 {
		return
	}
	lines := activityLines(feed)
	fmt.Fprintln(buf)
	fmt.Fprintln(buf, "## Activity")
	fmt.Fprintln(buf)
	for _, line := range lines {
		fmt.Fprintln(buf, line)
	}
	if len(feed) >= ActivityLimit {
		fmt.Fprintf(buf, "\n(latest %d entries)\n", ActivityLimit)
	}
}

// activityLines describes each entry, newest first. Bitbucket's feed is
// roughly newest first but places comments out of time order, so entries are
// sorted by time. Pushes show as updates whose source commit differs from
// the previous update's, so the feed is read oldest first.
func activityLines(feed []bitbucket.PullRequestActivity) []string {
	complete := len(feed) < ActivityLimit
	feed = slices.Clone(feed)
	slices.SortStableFunc(feed, func(a, b bitbucket.PullRequestActivity) int {
		return b.Time().Compare(a.Time())
	})
	lines := make([]string, 0, len(feed))
	previousSource := ""
	seenUpdate := false
	for i := len(feed) - 1; i >= 0; i-- {
		entry := feed[i]
		var what string
		switch entry.Kind() {
		case bitbucket.ActivityKindApproval:
			what = "approved"
		case bitbucket.ActivityKindChangesRequested:
			what = "requested changes"
		case bitbucket.ActivityKindComment:
			what = fmt.Sprintf("commented (#%d)", entry.Comment.ID)
			if entry.Comment.Inline != nil && entry.Comment.Inline.Path != "" {
				what = fmt.Sprintf("commented on %s (#%d)", entry.Comment.Inline.Path, entry.Comment.ID)
			}
		case bitbucket.ActivityKindUpdate:
			what = describeUpdate(entry.Update, previousSource, !seenUpdate && complete)
			previousSource = entry.Update.Source.Commit.Hash
			seenUpdate = true
		default:
			what = "other activity"
		}
		when := "unknown time"
		if t := entry.Time(); !t.IsZero() {
			when = t.UTC().Format(timeLayout)
		}
		actor := entry.Actor().DisplayName
		if actor == "" {
			actor = entry.Actor().Handle()
		}
		if what != "" {
			lines = append(lines, fmt.Sprintf("- %s · %s %s", when, actor, what))
		}
	}
	slices.Reverse(lines)
	return lines
}

// describeUpdate says what an update entry changed, or "" when it changed
// nothing visible (Bitbucket logs several such entries per event).
func describeUpdate(update *bitbucket.ActivityUpdate, previousSource string, first bool) string {
	source := shortHash(update.Source.Commit.Hash)
	if first {
		return fmt.Sprintf("opened the pull request (%s → %s at %s)", update.Source.Branch.Name, update.Destination.Branch.Name, source)
	}
	var parts []string
	if previousSource != "" && update.Source.Commit.Hash != previousSource {
		parts = append(parts, "pushed "+source)
	}
	keys := make([]string, 0, len(update.Changes))
	for key := range update.Changes {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		parts = append(parts, describeChange(key, update.Changes[key], update.State)...)
	}
	return strings.Join(parts, "; ")
}

func describeChange(key string, raw json.RawMessage, state string) []string {
	switch key {
	case "status":
		return []string{"set state to " + state}
	case "draft":
		var change struct {
			New bool `json:"new"`
		}
		if json.Unmarshal(raw, &change) == nil {
			if change.New {
				return []string{"marked draft"}
			}
			return []string{"marked ready for review"}
		}
	case "reviewers":
		var change struct {
			Added   []bitbucket.User `json:"added"`
			Removed []bitbucket.User `json:"removed"`
		}
		if json.Unmarshal(raw, &change) == nil {
			var parts []string
			for _, user := range change.Added {
				parts = append(parts, "added reviewer "+user.DisplayName)
			}
			for _, user := range change.Removed {
				parts = append(parts, "removed reviewer "+user.DisplayName)
			}
			if len(parts) > 0 {
				return parts
			}
		}
	case "description":
		return []string{"edited the description"}
	}
	return []string{"changed " + key}
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
