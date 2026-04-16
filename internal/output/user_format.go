package output

import (
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

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
