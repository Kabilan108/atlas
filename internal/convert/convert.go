package convert

import (
	"fmt"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

func HtmlToMarkdown(htmlContent string) (string, error) {
	if strings.TrimSpace(htmlContent) == "" {
		return "", nil
	}

	converter := md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		return "", fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}

	markdown = strings.TrimSpace(markdown)

	return markdown, nil
}
