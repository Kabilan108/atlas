// Package adf converts between Jira's Atlassian Document Format and markdown
// on top of github.com/nyactl/go-adf. On the way to markdown, media nodes
// become attachment names (linked when a URL is known) and nodes go-adf
// would drop or flatten silently become an explicit placeholder.
package adf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	goadf "github.com/nyactl/go-adf"
)

// Media is what a media or mediaInline node says about its file.
type Media struct {
	ID         string
	Type       string
	Collection string
	Alt        string
	URL        string
}

type config struct {
	mediaLink       func(Media) (name, url string)
	mentionText     func(accountID, text string) string
	mentionResolver func(label string) (accountID, text string, ok bool)
}

type Option func(*config)

// WithMediaLink names a media node and gives its URL ("" for none). Media
// IDs are Media Services IDs, not attachment IDs; Alt usually holds the
// attachment's file name.
func WithMediaLink(f func(Media) (name, url string)) Option {
	return func(c *config) { c.mediaLink = f }
}

// WithAttachmentURLs links media whose file name (Alt) is a key of urls.
func WithAttachmentURLs(urls map[string]string) Option {
	return WithMediaLink(func(m Media) (string, string) {
		if u, ok := urls[m.Alt]; ok {
			return m.Alt, u
		}
		return m.Alt, m.URL
	})
}

// WithMentionText sets the label written as @[label] for a mention; text is
// the stored display text, usually "@Name".
func WithMentionText(f func(accountID, text string) string) Option {
	return func(c *config) { c.mentionText = f }
}

// WithMentionResolver makes FromMarkdown turn @[label] into mention nodes.
// Without it @[label] stays text.
func WithMentionResolver(f func(label string) (accountID, text string, ok bool)) Option {
	return func(c *config) { c.mentionResolver = f }
}

func newConfig(opts []Option) config {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c
}

// ToMarkdown renders a Jira ADF value as markdown. Empty and null input give
// "", and a JSON string (some fields hold plain text) is returned as is.
func ToMarkdown(raw json.RawMessage, opts ...Option) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] == '"' {
		return goadf.ToMarkdown(trimmed)
	}
	var doc goadf.Node
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return "", fmt.Errorf("adf: parse document: %w", err)
	}
	p := &preparer{config: newConfig(opts), tokens: !bytes.Contains(trimmed, []byte(mentionToken))}
	prepared, err := json.Marshal(p.prepareRoot(doc))
	if err != nil {
		return "", err
	}
	var gopts []goadf.Option
	if p.mentionText != nil {
		gopts = append(gopts, goadf.WithMentionText(p.mentionText))
	}
	md, err := goadf.ToMarkdown(prepared, gopts...)
	if err != nil {
		return "", err
	}
	return p.restoreMentions(md), nil
}

// go-adf v0.2.0 checks that emphasis reads back by re-parsing the paragraph
// without a mention resolver, so a paragraph holding a mention always fails
// the check and loses its bold and italics. Mentions are therefore rendered
// as inert tokens and put back afterwards.
const mentionToken = "atlasmention"

func (p *preparer) mention(n goadf.Node) goadf.Node {
	if !p.tokens {
		return n
	}
	label := strings.TrimPrefix(attr(n, "text"), "@")
	if p.mentionText != nil {
		label = p.mentionText(attr(n, "id"), attr(n, "text"))
	}
	if label == "" {
		label = attr(n, "id")
	}
	p.mentions = append(p.mentions, "@["+label+"]")
	return text(fmt.Sprintf("%s%dx", mentionToken, len(p.mentions)-1))
}

func (p *preparer) restoreMentions(md string) string {
	for i := len(p.mentions) - 1; i >= 0; i-- {
		md = strings.ReplaceAll(md, fmt.Sprintf("%s%dx", mentionToken, i), p.mentions[i])
	}
	return md
}

// FromMarkdown converts markdown to an ADF document for Jira's v3 API.
func FromMarkdown(markdown string, opts ...Option) (json.RawMessage, error) {
	cfg := newConfig(opts)
	var gopts []goadf.Option
	if cfg.mentionResolver != nil {
		gopts = append(gopts, goadf.WithMentionResolver(cfg.mentionResolver))
	}
	return goadf.FromMarkdown(markdown, gopts...)
}

// known lists the node types go-adf renders with their meaning intact.
var known = map[string]bool{
	"doc": true, "paragraph": true, "text": true, "hardBreak": true, "heading": true,
	"bulletList": true, "orderedList": true, "listItem": true, "codeBlock": true,
	"blockquote": true, "rule": true, "table": true, "tableRow": true,
	"tableHeader": true, "tableCell": true, "mention": true, "emoji": true,
	"inlineCard": true, "blockCard": true, "embedCard": true, "date": true,
	"status": true, "panel": true, "expand": true, "nestedExpand": true,
	"taskList": true, "taskItem": true, "blockTaskItem": true, "decisionList": true,
	"decisionItem": true, "caption": true, "layoutSection": true, "layoutColumn": true,
	"bodiedExtension": true, "bodiedSyncBlock": true, "placeholder": true,
}

// inlineParents hold inline content; everything else holds blocks.
var inlineParents = map[string]bool{
	"paragraph": true, "heading": true, "caption": true, "taskItem": true,
	"decisionItem": true, "codeBlock": true,
}

type preparer struct {
	config
	tokens   bool
	mentions []string
}

func (c *preparer) prepareRoot(n goadf.Node) goadf.Node {
	if n.Type != "doc" {
		return goadf.Node{Type: "doc", Version: 1, Content: c.prepare(n, false)}
	}
	n.Content = c.prepareChildren(n)
	return n
}

func (c *preparer) prepareChildren(n goadf.Node) []goadf.Node {
	inline := inlineParents[n.Type]
	out := make([]goadf.Node, 0, len(n.Content))
	for _, child := range n.Content {
		out = append(out, c.prepare(child, inline)...)
	}
	return out
}

// prepare rewrites one node for go-adf; inline says whether the parent
// holds inline content.
func (c *preparer) prepare(n goadf.Node, inline bool) []goadf.Node {
	switch n.Type {
	case "mention":
		return []goadf.Node{c.mention(n)}
	case "media", "mediaInline":
		node := c.mediaInline(n)
		if inline {
			return []goadf.Node{node}
		}
		return []goadf.Node{paragraph(node)}
	case "mediaSingle", "mediaGroup":
		var parts []goadf.Node
		for _, child := range n.Content {
			if len(parts) > 0 {
				parts = append(parts, text(" "))
			}
			if child.Type == "caption" {
				parts = append(parts, c.prepareChildren(child)...)
				continue
			}
			parts = append(parts, c.prepare(child, true)...)
		}
		if inline {
			return parts
		}
		return []goadf.Node{paragraph(parts...)}
	}
	if known[n.Type] {
		n.Content = c.prepareChildren(n)
		return []goadf.Node{n}
	}

	marker := placeholder(n)
	if inline {
		inner := c.prepareChildren(goadf.Node{Type: "paragraph", Content: n.Content})
		if len(inner) == 0 {
			return []goadf.Node{marker}
		}
		return append([]goadf.Node{marker, text(" ")}, inner...)
	}
	if len(n.Content) > 0 && isInlineType(n.Content[0].Type) {
		inner := c.prepareChildren(goadf.Node{Type: "paragraph", Content: n.Content})
		return []goadf.Node{paragraph(append([]goadf.Node{marker, text(" ")}, inner...)...)}
	}
	return append([]goadf.Node{paragraph(marker)}, c.prepareChildren(goadf.Node{Type: "doc", Content: n.Content})...)
}

func (c *preparer) mediaInline(n goadf.Node) goadf.Node {
	m := Media{
		ID:         attr(n, "id"),
		Type:       attr(n, "type"),
		Collection: attr(n, "collection"),
		Alt:        attr(n, "alt"),
		URL:        attr(n, "url"),
	}
	name, url := m.Alt, m.URL
	if c.mediaLink != nil {
		name, url = c.mediaLink(m)
	}
	if name == "" && m.ID != "" {
		name = "media " + m.ID
	}
	if url != "" {
		if name == "" {
			name = url
		}
		return goadf.Node{Type: "text", Text: "attachment: " + name, Marks: []goadf.Mark{{Type: "link", Attrs: map[string]any{"href": url}}}}
	}
	node := goadf.Node{Type: "mediaInline"}
	if name != "" {
		node.Attrs = map[string]any{"alt": name}
	}
	return node
}

func placeholder(n goadf.Node) goadf.Node {
	label := "unsupported ADF node: " + n.Type
	if key := attr(n, "extensionKey"); key != "" {
		label += " (" + key + ")"
	}
	return goadf.Node{Type: "text", Text: "[" + label + "]", Marks: []goadf.Mark{{Type: "code"}}}
}

func isInlineType(t string) bool {
	switch t {
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "date", "status", "mediaInline", "placeholder", "inlineExtension":
		return true
	}
	return false
}

func paragraph(content ...goadf.Node) goadf.Node {
	return goadf.Node{Type: "paragraph", Content: content}
}

func text(s string) goadf.Node {
	return goadf.Node{Type: "text", Text: s}
}

func attr(n goadf.Node, key string) string {
	if v, ok := n.Attrs[key].(string); ok {
		return v
	}
	return ""
}
