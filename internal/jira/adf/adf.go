// Package adf converts between Jira's Atlassian Document Format and markdown
// on top of github.com/nyactl/go-adf. On the way to markdown, media nodes
// become attachment names (linked when a URL is known) and nodes go-adf
// would drop or flatten silently become an explicit placeholder.
package adf

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
// attachment's file name. A node f gives neither a name nor a URL renders
// as a bare [attachment].
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
	cfg := newConfig(opts)
	for range 4 {
		nonce, err := newNonce()
		if err != nil {
			return "", err
		}
		p := &preparer{config: cfg, nonce: nonce}
		prepared, err := json.Marshal(p.prepareRoot(doc))
		if err != nil {
			return "", err
		}
		if bytes.Count(prepared, []byte(nonce)) != len(p.tokens) {
			continue
		}
		md, err := goadf.ToMarkdown(prepared)
		if err != nil {
			return "", err
		}
		return p.restoreTokens(md), nil
	}
	return "", errors.New("adf: document text keeps colliding with mention placeholders")
}

// go-adf v0.2.0 checks that emphasis reads back by re-parsing the paragraph
// without a mention resolver, so a paragraph holding a mention always fails
// the check and loses its bold and italics. Mentions, and the checkboxes of
// task lists rendered as nested bullets, are therefore written as inert
// tokens and put back afterwards. Each call picks a random nonce and retries
// if the decoded document, as handed to go-adf, already contains it.
var newNonce = func() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("adf: mention placeholder: %w", err)
	}
	return "atlasmention" + hex.EncodeToString(b[:]), nil
}

func (p *preparer) token(i int) string {
	return fmt.Sprintf("%s%dx", p.nonce, i)
}

// placeholderFor returns a text node that restoreTokens replaces with s.
func (p *preparer) placeholderFor(s string) goadf.Node {
	p.tokens = append(p.tokens, s)
	return text(p.token(len(p.tokens) - 1))
}

func (p *preparer) mention(n goadf.Node) goadf.Node {
	label := strings.TrimPrefix(attr(n, "text"), "@")
	if p.mentionText != nil {
		label = p.mentionText(attr(n, "id"), attr(n, "text"))
	}
	if label == "" {
		label = attr(n, "id")
	}
	return p.placeholderFor("@[" + label + "]")
}

// restoreTokens replaces every token in one pass, so a label that looks
// like a token is never expanded again.
func (p *preparer) restoreTokens(md string) string {
	pairs := make([]string, 0, 2*len(p.tokens))
	for i, m := range p.tokens {
		pairs = append(pairs, p.token(i), m)
	}
	return strings.NewReplacer(pairs...).Replace(md)
}

// FromMarkdown converts markdown to an ADF document for Jira's v3 API.
// Bullets that start with "[ ]" or "[x]" become native task items.
func FromMarkdown(markdown string, opts ...Option) (json.RawMessage, error) {
	cfg := newConfig(opts)
	var gopts []goadf.Option
	if cfg.mentionResolver != nil {
		gopts = append(gopts, goadf.WithMentionResolver(cfg.mentionResolver))
	}
	raw, err := goadf.FromMarkdown(markdown, gopts...)
	if err != nil {
		return nil, err
	}
	return nativeTasks(raw)
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
	nonce  string
	tokens []string
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
	case "taskList":
		return []goadf.Node{c.taskBullets(n)}
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
	} else if name == "" && m.ID != "" {
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
