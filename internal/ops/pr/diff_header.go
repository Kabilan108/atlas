package pr

import (
	"bytes"
	"strconv"
	"strings"
)

// diffSection is one file's part of a git diff. A name is empty when the
// file is absent on that side (added or deleted).
type diffSection struct {
	oldName, newName string
	text             []byte
}

// diffSections splits a git diff into file sections and names each the way
// git apply does: rename/copy and ---/+++ headers win over the diff --git
// line, which is ambiguous for unquoted names with spaces. Text before the
// first diff --git line belongs to no section.
func diffSections(diff []byte) []diffSection {
	var sections []diffSection
	inHeader := false
	for _, line := range bytes.SplitAfter(diff, []byte("\n")) {
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if rest, ok := strings.CutPrefix(text, "diff --git "); ok {
			oldName, newName := gitHeaderNames(rest)
			sections = append(sections, diffSection{oldName: oldName, newName: newName})
			inHeader = true
		}
		if len(sections) == 0 {
			continue
		}
		current := &sections[len(sections)-1]
		current.text = append(current.text, line...)
		if !inHeader {
			continue
		}
		switch {
		case strings.HasPrefix(text, "@@"):
			inHeader = false
		case strings.HasPrefix(text, "rename from "), strings.HasPrefix(text, "copy from "):
			current.oldName = extendedHeaderName(text)
		case strings.HasPrefix(text, "rename to "), strings.HasPrefix(text, "copy to "):
			current.newName = extendedHeaderName(text)
		case strings.HasPrefix(text, "--- "):
			current.oldName = patchName(text[4:], "a/")
		case strings.HasPrefix(text, "+++ "):
			current.newName = patchName(text[4:], "b/")
			inHeader = false
		}
	}
	return sections
}

// gitHeaderNames reads the two names after "diff --git ". Quoted names are
// exact. Two unquoted names are split where both halves name the same file,
// as git does, and otherwise at the first " b/"; renames correct that from
// their rename headers.
func gitHeaderNames(rest string) (string, string) {
	if strings.HasPrefix(rest, `"`) {
		oldName, after, ok := cUnquote(rest)
		if !ok || !strings.HasPrefix(after, " ") {
			return "", ""
		}
		return stripDiffPrefix(oldName, "a/"), headerName(after[1:], "b/")
	}
	for i := strings.Index(rest, ` "`); i >= 0; {
		if newName, after, ok := cUnquote(rest[i+1:]); ok && after == "" {
			return headerName(rest[:i], "a/"), stripDiffPrefix(newName, "b/")
		}
		next := strings.Index(rest[i+1:], ` "`)
		if next < 0 {
			break
		}
		i += 1 + next
	}
	if mid := len(rest) / 2; len(rest)%2 == 1 && rest[mid] == ' ' &&
		strings.HasPrefix(rest, "a/") && strings.HasPrefix(rest[mid+1:], "b/") && rest[2:mid] == rest[mid+3:] {
		return rest[2:mid], rest[mid+3:]
	}
	if i := strings.Index(rest, " b/"); i >= 0 {
		return headerName(rest[:i], "a/"), headerName(rest[i+1:], "b/")
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		return headerName(rest[:i], "a/"), headerName(rest[i+1:], "b/")
	}
	return "", ""
}

func headerName(name, prefix string) string {
	if name == "/dev/null" {
		return ""
	}
	return stripDiffPrefix(name, prefix)
}

// extendedHeaderName reads the name of a rename or copy header, which has
// no a/ or b/ prefix.
func extendedHeaderName(line string) string {
	name := line[strings.Index(line, " ")+1:]
	name = name[strings.Index(name, " ")+1:]
	if unquoted, _, ok := cUnquote(name); ok {
		return unquoted
	}
	return name
}

// patchName reads the name on a --- or +++ line. git ends an unquoted name
// that has spaces with a tab, and other tools put a timestamp after one.
func patchName(field, prefix string) string {
	if unquoted, _, ok := cUnquote(field); ok {
		return headerName(unquoted, prefix)
	}
	if i := strings.IndexByte(field, '\t'); i >= 0 {
		field = field[:i]
	}
	return headerName(field, prefix)
}

func stripDiffPrefix(name, prefix string) string {
	if trimmed, ok := strings.CutPrefix(name, prefix); ok {
		return trimmed
	}
	return name
}

// cUnquote reads the C-style quoted name git writes for names with special
// characters (\t, \", \\, octal bytes) at the start of s, and returns what
// follows it.
func cUnquote(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			name, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", s, false
			}
			return name, s[i+1:], true
		}
	}
	return "", s, false
}
