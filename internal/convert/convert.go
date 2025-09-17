package convert

import html2md "github.com/JohannesKaufmann/html-to-markdown"

// HtmlToMarkdown converts HTML content into Markdown using the shared converter.
func HtmlToMarkdown(html string) (string, error) {
	converter := html2md.NewConverter("", true, nil)
	return converter.ConvertString(html)
}
