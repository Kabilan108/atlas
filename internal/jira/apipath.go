package jira

import (
	"net/url"
	"strings"
	"unicode"
)

// apiPrefixes are the Jira REST families a read-only passthrough may reach
// through the gateway.
var apiPrefixes = []string{
	"/rest/api/3/",
	"/rest/api/2/",
	"/rest/agile/1.0/",
	"/rest/servicedeskapi/",
}

// APIPathAllowed reports whether path, as a caller would append it to the
// client's base URL (with an optional query string), is a Jira REST API
// path that a GET-only passthrough may request: under /rest/api/3, 2,
// /rest/agile/1.0 or /rest/servicedeskapi, with no scheme or host, no dot
// segments, no backslashes or encoded slashes, and no control characters.
// It does not decide which method is safe; callers allow GET only.
func APIPathAllowed(path string) bool {
	rawPath, _, _ := strings.Cut(path, "?")
	if rawPath == "" || strings.ContainsFunc(path, unicode.IsControl) || strings.Contains(path, "#") {
		return false
	}
	if strings.ContainsAny(rawPath, `\`) || strings.Contains(strings.ToLower(rawPath), "%2f") || strings.Contains(strings.ToLower(rawPath), "%5c") {
		return false
	}
	decoded, err := url.PathUnescape(rawPath)
	if err != nil || !strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, "//") {
		return false
	}
	for segment := range strings.SplitSeq(decoded, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(decoded, prefix) && len(decoded) > len(prefix) {
			return true
		}
	}
	return false
}
