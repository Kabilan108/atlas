package jira

import (
	"fmt"
	"strings"
	"time"
)

// TimeLayout is the timestamp format Jira sends and expects for worklog
// start times.
const TimeLayout = "2006-01-02T15:04:05.000-0700"

var inputLayouts = []string{
	TimeLayout,
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999-0700",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04-0700",
}

// ParseTime reads Jira's format and RFC 3339 / ISO 8601 timestamps with a
// UTC offset ("Z", "-04:00" or "-0400"), with or without seconds and
// fractions. A timestamp without an offset is rejected rather than guessed.
func ParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range inputLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("jira: cannot read time %q; use ISO 8601 with an offset, e.g. 2026-11-11T09:30:00-04:00", s)
}

// FormatTime writes t in Jira's format, keeping t's offset.
func FormatTime(t time.Time) string {
	return t.Format(TimeLayout)
}

// NormalizeTime converts an ISO 8601 timestamp to Jira's format.
func NormalizeTime(s string) (string, error) {
	t, err := ParseTime(s)
	if err != nil {
		return "", err
	}
	return FormatTime(t), nil
}
