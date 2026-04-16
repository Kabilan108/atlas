package output

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

var bitbucketMentionPattern = regexp.MustCompile(`@\{([^}]+)\}`)

var markdownTextEscaper = strings.NewReplacer(
	"\\", "\\\\",
	"`", "\\`",
	"*", "\\*",
	"_", "\\_",
	"{", "\\{",
	"}", "\\}",
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"<", "\\<",
	">", "\\>",
)

func formatUserMention(user bitbucket.User) string {
	return "@" + escapeMarkdownText(user.Handle())
}

func escapeMarkdownText(text string) string {
	return markdownTextEscaper.Replace(text)
}

func normalizeMarkdownText(text string, resolveMention func(string) (bitbucket.User, bool)) string {
	text = strings.ReplaceAll(text, "\u200c", "")
	if resolveMention == nil {
		return text
	}

	var builder strings.Builder
	lines := strings.SplitAfter(text, "\n")
	inFence := false

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if isFenceDelimiter(trimmed) {
			inFence = !inFence
			builder.WriteString(line)
			continue
		}

		if inFence {
			builder.WriteString(line)
			continue
		}

		builder.WriteString(resolveMentionsOutsideInlineCode(line, resolveMention))
	}

	return builder.String()
}

func resolveMentionsOutsideInlineCode(text string, resolveMention func(string) (bitbucket.User, bool)) string {
	var builder strings.Builder

	for i := 0; i < len(text); {
		if text[i] != '`' {
			next := strings.IndexByte(text[i:], '`')
			if next == -1 {
				builder.WriteString(replaceBitbucketMentions(text[i:], resolveMention))
				break
			}
			builder.WriteString(replaceBitbucketMentions(text[i:i+next], resolveMention))
			i += next
			continue
		}

		delimiterLen := backtickRunLength(text[i:])
		end := findClosingBackticks(text, i+delimiterLen, delimiterLen)
		if end == -1 {
			builder.WriteString(replaceBitbucketMentions(text[i:], resolveMention))
			break
		}

		builder.WriteString(text[i : end+delimiterLen])
		i = end + delimiterLen
	}

	return builder.String()
}

func replaceBitbucketMentions(text string, resolveMention func(string) (bitbucket.User, bool)) string {
	return bitbucketMentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := bitbucketMentionPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}

		user, ok := resolveMention(parts[1])
		if !ok {
			return match
		}

		return formatUserMention(user)
	})
}

func isFenceDelimiter(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

func backtickRunLength(text string) int {
	length := 0
	for length < len(text) && text[length] == '`' {
		length++
	}
	return length
}

func findClosingBackticks(text string, start, delimiterLen int) int {
	for i := start; i < len(text); {
		if text[i] == '`' {
			if backtickRunLength(text[i:]) == delimiterLen {
				return i
			}
		}

		_, width := utf8.DecodeRuneInString(text[i:])
		i += width
	}

	return -1
}
