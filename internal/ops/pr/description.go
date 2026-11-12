package pr

import (
	"net/url"
	"strings"
)

type markdownLine struct {
	text    string
	newline string
}

type markdownFence struct {
	marker byte
	length int
}

const bitbucketInlineCardSuffix = "{: data-inline-card='' }"

// NormalizeDescription adjusts markdown so Bitbucket renders it as written:
// a blank line between a "Label:" line and the list after it, and a
// standalone Bitbucket or Jira URL as an inline card. Fenced code is left
// alone, and a body that needs neither change is returned unchanged.
func NormalizeDescription(body string) string {
	lines := splitMarkdownLines(body)
	if len(lines) == 0 {
		return body
	}

	var normalized strings.Builder
	normalized.Grow(len(body))
	inFence := false
	openFence := markdownFence{}
	changed := false
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line.text)
		lineInFence := inFence
		currentFence, hasCurrentFence := parseMarkdownFence(trimmedLine)
		if hasCurrentFence {
			if inFence && currentFence.closes(openFence) {
				inFence = false
			} else if !inFence {
				inFence = true
				openFence = currentFence
			}
		}

		lineText := line.text
		if !lineInFence && !hasCurrentFence {
			var normalizedInlineCard bool
			lineText, normalizedInlineCard = normalizeInlineCardLine(lineText)
			changed = changed || normalizedInlineCard
		}

		normalized.WriteString(lineText)
		normalized.WriteString(line.newline)
		if line.newline == "" || lineInFence || hasCurrentFence || i+1 >= len(lines) {
			continue
		}
		if isPRSectionLabel(trimmedLine) && isMarkdownListItem(lines[i+1].text) {
			normalized.WriteString(line.newline)
			changed = true
		}
	}
	if !changed {
		return body
	}
	return normalized.String()
}

func normalizeInlineCardLine(line string) (string, bool) {
	if strings.Contains(line, bitbucketInlineCardSuffix) {
		return line, false
	}

	if normalized, ok := normalizePlainInlineCardLine(line); ok {
		return normalized, true
	}

	prefix, content, ok := splitMarkdownListContent(line)
	if !ok {
		return line, false
	}
	normalized, ok := normalizePlainInlineCardLine(content)
	if !ok {
		return line, false
	}
	return prefix + normalized, true
}

func normalizePlainInlineCardLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !isInlineCardURL(trimmed) {
		return line, false
	}
	leadingLength := len(line) - len(strings.TrimLeft(line, " \t"))
	trailingLength := len(line) - len(strings.TrimRight(line, " \t"))
	return line[:leadingLength] + formatInlineCardLink(trimmed) + line[len(line)-trailingLength:], true
}

func isInlineCardURL(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	hostname := strings.ToLower(parsedURL.Hostname())
	return hostname == "bitbucket.org" || strings.HasSuffix(hostname, ".atlassian.net")
}

func formatInlineCardLink(rawURL string) string {
	return "[" + rawURL + "](" + rawURL + ")" + bitbucketInlineCardSuffix
}

func splitMarkdownLines(text string) []markdownLine {
	if text == "" {
		return nil
	}
	lines := make([]markdownLine, 0, strings.Count(text, "\n")+1)
	start := 0
	for start < len(text) {
		newlineIndex := strings.IndexByte(text[start:], '\n')
		if newlineIndex == -1 {
			lines = append(lines, markdownLine{text: text[start:]})
			return lines
		}
		end := start + newlineIndex
		lineText := text[start:end]
		newline := "\n"
		if strings.HasSuffix(lineText, "\r") {
			lineText = strings.TrimSuffix(lineText, "\r")
			newline = "\r\n"
		}
		lines = append(lines, markdownLine{text: lineText, newline: newline})
		start = end + 1
	}
	return lines
}

func isPRSectionLabel(line string) bool {
	return line != "" && strings.HasSuffix(line, ":")
}

func splitMarkdownListContent(line string) (string, string, bool) {
	leadingLength := len(line) - len(strings.TrimLeft(line, " \t"))
	remaining := line[leadingLength:]
	if len(remaining) < 3 {
		return "", "", false
	}

	switch remaining[0] {
	case '-', '*', '+':
		if !isMarkdownWhitespace(remaining[1]) {
			return "", "", false
		}
		contentStart := consumeMarkdownWhitespace(remaining, 1)
		return line[:leadingLength] + remaining[:contentStart], remaining[contentStart:], true
	}

	digitEnd := 0
	for digitEnd < len(remaining) && remaining[digitEnd] >= '0' && remaining[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 || digitEnd+1 >= len(remaining) {
		return "", "", false
	}
	if remaining[digitEnd] != '.' && remaining[digitEnd] != ')' {
		return "", "", false
	}
	if !isMarkdownWhitespace(remaining[digitEnd+1]) {
		return "", "", false
	}
	contentStart := consumeMarkdownWhitespace(remaining, digitEnd+1)
	return line[:leadingLength] + remaining[:contentStart], remaining[contentStart:], true
}

func isMarkdownListItem(line string) bool {
	trimmedLine := strings.TrimLeft(line, " \t")
	if len(trimmedLine) < 3 {
		return false
	}
	switch trimmedLine[0] {
	case '-', '*', '+':
		return isMarkdownWhitespace(trimmedLine[1])
	}

	digitEnd := 0
	for digitEnd < len(trimmedLine) && trimmedLine[digitEnd] >= '0' && trimmedLine[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 || digitEnd+1 >= len(trimmedLine) {
		return false
	}
	if trimmedLine[digitEnd] != '.' && trimmedLine[digitEnd] != ')' {
		return false
	}
	return isMarkdownWhitespace(trimmedLine[digitEnd+1])
}

func isMarkdownWhitespace(char byte) bool {
	return char == ' ' || char == '\t'
}

func consumeMarkdownWhitespace(text string, start int) int {
	for start < len(text) && isMarkdownWhitespace(text[start]) {
		start++
	}
	return start
}

func parseMarkdownFence(line string) (markdownFence, bool) {
	if len(line) < 3 {
		return markdownFence{}, false
	}
	marker := line[0]
	if marker != '`' && marker != '~' {
		return markdownFence{}, false
	}
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	if length < 3 {
		return markdownFence{}, false
	}
	return markdownFence{marker: marker, length: length}, true
}

func (f markdownFence) closes(open markdownFence) bool {
	return f.marker == open.marker && f.length >= open.length
}
