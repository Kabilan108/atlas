package output

import (
	"strings"
)

// MarkdownTable renders a GitHub-style table. Pipes and newlines in cells are
// escaped so a cell cannot break the table.
func MarkdownTable(headers []string, rows [][]string) string {
	var b strings.Builder
	writeMarkdownRow(&b, headers)
	b.WriteString("|")
	for range headers {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range rows {
		writeMarkdownRow(&b, row)
	}
	return b.String()
}

func writeMarkdownRow(b *strings.Builder, cells []string) {
	b.WriteString("|")
	for _, cell := range cells {
		b.WriteString(" ")
		b.WriteString(markdownCell(cell))
		b.WriteString(" |")
	}
	b.WriteString("\n")
}

func markdownCell(cell string) string {
	cell = strings.ReplaceAll(cell, `\`, `\\`)
	cell = strings.ReplaceAll(cell, "|", `\|`)
	cell = strings.ReplaceAll(cell, "\r\n", " ")
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(cell)
}

// CodeFence wraps content in a backtick fence longer than any backtick run
// inside it, so the content cannot close the fence early.
func CodeFence(content, lang string) string {
	fence := strings.Repeat("`", max(3, longestBacktickRun(content)+1))
	var b strings.Builder
	b.WriteString(fence + lang + "\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fence + "\n")
	return b.String()
}

func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return longest
}

// FenceLanguage guesses a fence info string from a file name's extension, or
// returns "" when the extension is missing or unusual.
func FenceLanguage(name string) string {
	base := name[strings.LastIndex(name, "/")+1:]
	dot := strings.LastIndex(base, ".")
	if dot <= 0 || dot == len(base)-1 {
		return ""
	}
	ext := strings.ToLower(base[dot+1:])
	if alias, ok := fenceAliases[ext]; ok {
		return alias
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	if len(ext) > 10 {
		return ""
	}
	return ext
}

var fenceAliases = map[string]string{
	"yml": "yaml",
	"md":  "markdown",
	"sh":  "sh",
	"py":  "python",
	"rb":  "ruby",
	"js":  "javascript",
	"ts":  "typescript",
	"rs":  "rust",
	"txt": "",
}
