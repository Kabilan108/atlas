package adf

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const descriptionMarkdown = "## Steps to reproduce\n\n" +
	"Run `make test` on **main** and *watch* the [build log](https://ci.example.com/build/42). cc @[Mia Krystof]\n\n" +
	"- first bullet\n- second bullet\n\n" +
	"1. step one\n2. step two\n\n" +
	"- [x] write the fix\n- [ ] ship it\n\n" +
	"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n" +
	"| Env | Result |\n| --- | --- |\n| staging | fails |\n\n" +
	"[attachment: screenshot.png]\n\n" +
	"`[unsupported ADF node: extension (roadmap)]`\n\n" +
	"`[unsupported ADF node: futureWidget]`\n\n" +
	"kept text"

func readFixture(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/description.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestToMarkdownFixture(t *testing.T) {
	got, err := ToMarkdown(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != descriptionMarkdown {
		t.Errorf("ToMarkdown =\n%s\n\nwant\n%s", got, descriptionMarkdown)
	}
}

func TestToMarkdownLinksAttachments(t *testing.T) {
	got, err := ToMarkdown(readFixture(t), WithAttachmentURLs(map[string]string{
		"screenshot.png": "https://example.atlassian.net/rest/api/3/attachment/content/10000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := "[attachment: screenshot.png](https://example.atlassian.net/rest/api/3/attachment/content/10000)"
	if !strings.Contains(got, want) {
		t.Errorf("ToMarkdown missing %q:\n%s", want, got)
	}
}

func TestToMarkdownMediaVariants(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		opts []Option
		want string
	}{
		{
			name: "media without alt uses its id",
			doc:  `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"6e7c","type":"file","collection":""}}]}]}`,
			want: "[attachment: media 6e7c]",
		},
		{
			name: "media group with caption",
			doc: `{"type":"doc","version":1,"content":[{"type":"mediaGroup","content":[
				{"type":"media","attrs":{"id":"a","type":"file","collection":"","alt":"one.log"}},
				{"type":"media","attrs":{"id":"b","type":"file","collection":"","alt":"two.log"}}]},
				{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"c","type":"file","alt":"chart.png"}},{"type":"caption","content":[{"type":"text","text":"Latency"}]}]}]}`,
			want: "[attachment: one.log] [attachment: two.log]\n\n[attachment: chart.png] Latency",
		},
		{
			name: "inline media in a paragraph",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"mediaInline","attrs":{"id":"d","type":"file","alt":"report.pdf"}}]}]}`,
			want: "see [attachment: report.pdf]",
		},
		{
			name: "external image links its url",
			doc:  `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"external","url":"https://img.example.com/a.png","alt":"a.png"}}]}]}`,
			want: "[attachment: a.png](https://img.example.com/a.png)",
		},
		{
			name: "custom media link",
			doc:  `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"e","type":"file"}}]}]}`,
			opts: []Option{WithMediaLink(func(m Media) (string, string) { return "file-" + m.ID, "https://x/" + m.ID })},
			want: "[attachment: file-e](https://x/e)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToMarkdown(json.RawMessage(tc.doc), tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToMarkdownUnknownNodesArePlaceholders(t *testing.T) {
	tests := []struct{ name, doc, want string }{
		{
			name: "inline extension",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Status: "},{"type":"inlineExtension","attrs":{"extensionType":"com.atlassian.ecosystem","extensionKey":"jira-status"}}]}]}`,
			want: "Status: `[unsupported ADF node: inlineExtension (jira-status)]`",
		},
		{
			name: "unknown inline with text",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"sparkle","content":[{"type":"text","text":"shiny"}]}]}]}`,
			want: "`[unsupported ADF node: sparkle]` shiny",
		},
		{
			name: "unknown block with inline content",
			doc:  `{"type":"doc","version":1,"content":[{"type":"callout","content":[{"type":"text","text":"careful"}]}]}`,
			want: "`[unsupported ADF node: callout]` careful",
		},
		{
			name: "sync block",
			doc:  `{"type":"doc","version":1,"content":[{"type":"syncBlock","attrs":{"resourceId":"r"}}]}`,
			want: "`[unsupported ADF node: syncBlock]`",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToMarkdown(json.RawMessage(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToMarkdownMentions(t *testing.T) {
	doc := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[
		{"type":"text","text":"ping "},{"type":"mention","attrs":{"id":"acc-1","text":"@Mia"}},
		{"type":"text","text":" and "},{"type":"mention","attrs":{"id":"acc-2","text":"@Emma"}},
		{"type":"text","text":" about "},{"type":"text","text":"this","marks":[{"type":"strong"}]}]}]}`)
	got, err := ToMarkdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ping @[Mia] and @[Emma] about **this**"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got, err = ToMarkdown(doc, WithMentionText(func(id, _ string) string { return "user:" + id }))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ping @[user:acc-1] and @[user:acc-2] about **this**"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToMarkdownTokenTextIsLeftAlone(t *testing.T) {
	doc := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"atlasmention0x "},{"type":"mention","attrs":{"id":"a","text":"@Mia"}}]}]}`)
	got, err := ToMarkdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "atlasmention0x @[Mia]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToMarkdownEdgeInputs(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"null", ""},
		{`"plain *text* field"`, "plain *text* field"},
	} {
		got, err := ToMarkdown(json.RawMessage(tc.in))
		if err != nil || got != tc.want {
			t.Errorf("ToMarkdown(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ToMarkdown(json.RawMessage(`{"type":`)); err == nil {
		t.Error("ToMarkdown(invalid) = nil error")
	}
}

type node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []node         `json:"content"`
	Text    string         `json:"text"`
	Marks   []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	} `json:"marks"`
}

func walk(n node, visit func(node)) {
	visit(n)
	for _, c := range n.Content {
		walk(c, visit)
	}
}

func TestFromMarkdownStructure(t *testing.T) {
	md := "## Steps\n\n" +
		"Run `make test` on **main**, *watch* the [log](https://ci.example.com/42). cc @[Mia Krystof]\n\n" +
		"- bullet\n\n1. step\n\n- [ ] todo\n- [x] done\n\n" +
		"```go\nfmt.Println(1)\n```\n\n" +
		"| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	raw, err := FromMarkdown(md, WithMentionResolver(func(label string) (string, string, bool) {
		return "acc-" + strings.ReplaceAll(label, " ", "-"), "@" + label, label == "Mia Krystof"
	}))
	if err != nil {
		t.Fatal(err)
	}
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Type != "doc" {
		t.Fatalf("root = %q", doc.Type)
	}
	types := map[string]int{}
	marks := map[string]bool{}
	var codeLang, href, mentionID string
	var headingLevel float64
	walk(doc, func(n node) {
		types[n.Type]++
		for _, m := range n.Marks {
			marks[m.Type] = true
			if m.Type == "link" {
				href, _ = m.Attrs["href"].(string)
			}
		}
		switch n.Type {
		case "codeBlock":
			codeLang, _ = n.Attrs["language"].(string)
		case "heading":
			headingLevel, _ = n.Attrs["level"].(float64)
		case "mention":
			mentionID, _ = n.Attrs["id"].(string)
		}
	})
	for _, want := range []string{"heading", "paragraph", "bulletList", "orderedList", "listItem", "codeBlock", "table", "tableRow", "tableHeader", "tableCell", "mention"} {
		if types[want] == 0 {
			t.Errorf("no %s node in %s", want, raw)
		}
	}
	for _, want := range []string{"code", "strong", "em", "link"} {
		if !marks[want] {
			t.Errorf("no %s mark in %s", want, raw)
		}
	}
	if codeLang != "go" || href != "https://ci.example.com/42" || headingLevel != 2 || mentionID != "acc-Mia-Krystof" {
		t.Errorf("lang=%q href=%q level=%v mention=%q", codeLang, href, headingLevel, mentionID)
	}
	if types["taskList"] != 1 || types["taskItem"] != 2 || strings.Contains(string(raw), "[ ]") {
		t.Errorf("checkbox bullets are not one native task list: %s", raw)
	}
}

func TestFromMarkdownUnknownMentionFails(t *testing.T) {
	_, err := FromMarkdown("hi @[Nobody]", WithMentionResolver(func(string) (string, string, bool) { return "", "", false }))
	if err == nil {
		t.Error("FromMarkdown with unknown mention = nil error")
	}
	raw, err := FromMarkdown("hi @[Nobody]")
	if err != nil || strings.Contains(string(raw), "mention") {
		t.Errorf("without resolver: %s, %v", raw, err)
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	resolver := WithMentionResolver(func(label string) (string, string, bool) { return "acc-" + label, "@" + label, true })
	inputs := []string{
		"## Heading\n\nText with **bold**, *italic*, `code` and a [link](https://example.com). cc @[Mia]",
		"- one\n- two\n  - nested\n\n1. first\n2. second",
		"```python\nprint('hi')\n```",
		"| Env | Result |\n| --- | --- |\n| staging | **fails** |",
		"> quoted\n\n---\n\nafter",
	}
	for _, md := range inputs {
		doc, err := FromMarkdown(md, resolver)
		if err != nil {
			t.Fatalf("FromMarkdown(%q): %v", md, err)
		}
		back, err := ToMarkdown(doc)
		if err != nil {
			t.Fatal(err)
		}
		again, err := FromMarkdown(back, resolver)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(doc) {
			t.Errorf("round trip changed the document:\n%s\n%s\n---\n%s\n%s", md, doc, back, again)
		}
	}
}

func TestADFRoundTripFixture(t *testing.T) {
	md, err := ToMarkdown(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := FromMarkdown(md, WithMentionResolver(func(label string) (string, string, bool) {
		return "5b10ac8d82e05b22cc7d4ef5", "@" + label, true
	}))
	if err != nil {
		t.Fatal(err)
	}
	again, err := ToMarkdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Everything but placeholders reads back the same: attachment and
	// unsupported-node placeholders come back as literal text.
	stable, _, _ := strings.Cut(md, "[attachment:")
	if !strings.HasPrefix(again, stable) {
		t.Errorf("round trip changed the supported part:\n%s\n---\n%s", stable, again)
	}
	if !strings.Contains(again, `\[attachment: screenshot.png\]`) {
		t.Errorf("known gaps changed; update the notes:\n%s", again)
	}
}

func TestToMarkdownMentionTokensCannotCollide(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		opts []Option
		want string
	}{
		{
			name: "escaped token text",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"` + `\` + `u0061tlasmention0x "},{"type":"mention","attrs":{"id":"a","text":"@Mia"}}]}]}`,
			want: "atlasmention0x @[Mia]",
		},
		{
			name: "literal prefix keeps emphasis",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"atlasmention "},{"type":"mention","attrs":{"id":"a","text":"@Mia"}},{"type":"text","text":" "},{"type":"text","text":"now","marks":[{"type":"strong"}]}]}]}`,
			want: "atlasmention @[Mia] **now**",
		},
		{
			name: "callback output looks like a token",
			doc:  `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a","text":"@Mia"}},{"type":"text","text":" "},{"type":"mention","attrs":{"id":"b","text":"@Emma"}}]}]}`,
			opts: []Option{WithMentionText(func(id, text string) string {
				if id == "b" {
					return "atlasmention0x"
				}
				return strings.TrimPrefix(text, "@")
			})},
			want: "@[Mia] @[atlasmention0x]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToMarkdown(json.RawMessage(tc.doc), tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToMarkdownRetriesCollidingNonce(t *testing.T) {
	orig := newNonce
	t.Cleanup(func() { newNonce = orig })
	nonces := []string{"atlasmentionfixed", "atlasmentionfresh"}
	calls := 0
	newNonce = func() (string, error) {
		n := nonces[min(calls, len(nonces)-1)]
		calls++
		return n, nil
	}
	doc := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"` + `\` + `u0061tlasmentionfixed0x "},{"type":"mention","attrs":{"id":"a","text":"@Mia"}}]}]}`)
	got, err := ToMarkdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "atlasmentionfixed0x @[Mia]"; got != want || calls != 2 {
		t.Errorf("got %q after %d nonces, want %q after 2", got, calls, want)
	}

	nonces = []string{"atlasmentionfixed"}
	calls = 0
	if _, err := ToMarkdown(doc); err == nil {
		t.Error("ToMarkdown with a nonce that always collides = nil error")
	}
}
