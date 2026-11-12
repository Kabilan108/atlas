package adf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	goadf "github.com/nyactl/go-adf"
)

// Approximations counts, by kind, the constructs in a document that
// ToMarkdown can show only approximately and that FromMarkdown cannot
// rebuild from that markdown: media, complex tables, underline and colour,
// expands, smart links, panels and the like. Kinds are short labels such as
// "media" and "complex table".
type Approximations map[string]int

// Approximate counts the constructs in an ADF value that a markdown round
// trip would lose. Empty, null and plain-string values have none.
func Approximate(raw json.RawMessage) (Approximations, error) {
	found := Approximations{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] == '"' {
		return found, nil
	}
	var doc goadf.Node
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("adf: parse document: %w", err)
	}
	found.walk(doc)
	return found, nil
}

// Total is the number of constructs counted.
func (a Approximations) Total() int {
	total := 0
	for _, n := range a {
		total += n
	}
	return total
}

// Add counts other's constructs into a.
func (a Approximations) Add(other Approximations) {
	for kind, n := range other {
		a[kind] += n
	}
}

// String lists the kinds by name with their counts, e.g.
// "complex table x1, media x2".
func (a Approximations) String() string {
	parts := make([]string, 0, len(a))
	for _, kind := range slices.Sorted(maps.Keys(a)) {
		if a[kind] > 0 {
			parts = append(parts, fmt.Sprintf("%s x%d", kind, a[kind]))
		}
	}
	return strings.Join(parts, ", ")
}

var approximateNodes = map[string]string{
	"media": "media", "mediaInline": "media",
	"expand": "expand", "nestedExpand": "expand",
	"inlineCard": "smart link", "blockCard": "smart link", "embedCard": "smart link",
	"panel": "panel", "status": "status", "decisionList": "decision",
	"layoutSection": "layout", "blockTaskItem": "block task",
	"extension": "extension", "bodiedExtension": "extension", "inlineExtension": "extension",
	"multiBodiedExtension": "extension", "bodiedSyncBlock": "sync block", "syncBlock": "sync block",
}

// containers hold other counted nodes and are not counted themselves.
var containers = map[string]bool{
	"mediaSingle": true, "mediaGroup": true, "caption": true, "decisionItem": true,
	"layoutColumn": true, "extensionFrame": true,
}

var approximateMarks = map[string]string{
	"underline": "underline", "textColor": "text color", "backgroundColor": "text color",
	"subsup": "subscript/superscript", "annotation": "inline comment",
	"alignment": "alignment", "indentation": "indentation",
}

func (a Approximations) walk(n goadf.Node) {
	switch kind, ok := approximateNodes[n.Type]; {
	case ok:
		a[kind]++
	case n.Type == "table" && complexTable(n):
		a["complex table"]++
	case !known[n.Type] && !containers[n.Type]:
		a["unsupported node"]++
	}
	for _, m := range n.Marks {
		if kind, ok := approximateMarks[m.Type]; ok {
			a[kind]++
		}
	}
	for _, child := range n.Content {
		a.walk(child)
	}
}

// complexTable reports whether a table has a shape markdown tables cannot
// hold: no header row, header cells below it, rows of different widths,
// merged cells, a numbering column, or a cell with more than one paragraph.
func complexTable(n goadf.Node) bool {
	if b, _ := n.Attrs["isNumberColumnEnabled"].(bool); b {
		return true
	}
	width := -1
	for i, row := range n.Content {
		if width >= 0 && len(row.Content) != width {
			return true
		}
		width = len(row.Content)
		for _, cell := range row.Content {
			if (cell.Type == "tableHeader") != (i == 0) {
				return true
			}
			if span(cell, "colspan") > 1 || span(cell, "rowspan") > 1 {
				return true
			}
			if len(cell.Content) > 1 || (len(cell.Content) == 1 && cell.Content[0].Type != "paragraph") {
				return true
			}
		}
	}
	return false
}

func span(n goadf.Node, key string) float64 {
	v, _ := n.Attrs[key].(float64)
	return v
}

// Append adds the blocks of addition, an ADF document, after the blocks of
// existing, which keeps every block it had exactly as Jira stored it. An
// empty or null existing value gives addition.
func Append(existing, addition json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(existing)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return addition, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("adf: parse existing document: %w", err)
	}
	var docType string
	if err := json.Unmarshal(doc["type"], &docType); err != nil || docType != "doc" {
		return nil, fmt.Errorf("adf: existing value is not an ADF document")
	}
	var content []json.RawMessage
	if len(doc["content"]) > 0 {
		if err := json.Unmarshal(doc["content"], &content); err != nil {
			return nil, fmt.Errorf("adf: parse existing content: %w", err)
		}
	}
	var add struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(addition, &add); err != nil {
		return nil, fmt.Errorf("adf: parse added document: %w", err)
	}
	merged, err := marshalRaw(append(content, add.Content...))
	if err != nil {
		return nil, err
	}
	doc["content"] = merged
	return marshalRaw(doc)
}

// marshalRaw encodes without HTML escaping, so existing text keeps its
// spelling.
func marshalRaw(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
