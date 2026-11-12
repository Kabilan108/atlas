package adf

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// shape writes a node tree compactly, with task states and text but without
// local IDs, so tests can compare structure.
func shape(n node) string {
	var b strings.Builder
	b.WriteString(n.Type)
	if state, ok := n.Attrs["state"].(string); ok {
		fmt.Fprintf(&b, "[%s]", state)
	}
	if n.Text != "" {
		fmt.Fprintf(&b, "%q", n.Text)
	}
	if len(n.Content) > 0 {
		parts := make([]string, len(n.Content))
		for i, c := range n.Content {
			parts[i] = shape(c)
		}
		fmt.Fprintf(&b, "(%s)", strings.Join(parts, " "))
	}
	return b.String()
}

func parse(t *testing.T, raw json.RawMessage) node {
	t.Helper()
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestFromMarkdownNativeTasks(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want string
	}{
		{
			name: "flat",
			md:   "- [ ] todo\n- [x] done\n- [X] also done",
			want: `doc(taskList(taskItem[TODO](text"todo") taskItem[DONE](text"done") taskItem[DONE](text"also done")))`,
		},
		{
			name: "nested tasks stay nested",
			md:   "- [ ] parent\n  - [x] child\n  - [ ] child two\n- [ ] next",
			want: `doc(taskList(taskItem[TODO](text"parent") taskList(taskItem[DONE](text"child") taskItem[TODO](text"child two")) taskItem[TODO](text"next")))`,
		},
		{
			name: "marks after the checkbox",
			md:   "- [ ] **bold** task",
			want: `doc(taskList(taskItem[TODO](text"bold" text" task")))`,
		},
		{
			name: "mixed list splits into runs",
			md:   "- [ ] a\n- plain\n- [x] b",
			want: `doc(taskList(taskItem[TODO](text"a")) bulletList(listItem(paragraph(text"plain"))) taskList(taskItem[DONE](text"b")))`,
		},
		{
			name: "tasks under a plain bullet",
			md:   "- plain\n  - [ ] sub",
			want: `doc(bulletList(listItem(paragraph(text"plain") taskList(taskItem[TODO](text"sub")))))`,
		},
		{
			name: "task with a plain sublist stays a bullet",
			md:   "- [ ] parent\n  - plain child",
			want: `doc(bulletList(listItem(paragraph(text"[ ] parent") bulletList(listItem(paragraph(text"plain child"))))))`,
		},
		{
			name: "blockquotes cannot hold task lists",
			md:   "> - [ ] quoted",
			want: `doc(blockquote(bulletList(listItem(paragraph(text"[ ] quoted")))))`,
		},
		{
			name: "table cells",
			md:   "| A |\n| --- |\n| x |",
			want: `doc(table(tableRow(tableHeader(paragraph(text"A"))) tableRow(tableCell(paragraph(text"x")))))`,
		},
		{
			name: "ordered lists are left alone",
			md:   "1. [ ] step",
			want: `doc(orderedList(listItem(paragraph(text"[ ] step"))))`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := FromMarkdown(tc.md)
			if err != nil {
				t.Fatal(err)
			}
			if got := shape(parse(t, raw)); got != tc.want {
				t.Errorf("FromMarkdown(%q)\n got %s\nwant %s", tc.md, got, tc.want)
			}
			doc := parse(t, raw)
			ids := map[string]bool{}
			walk(doc, func(n node) {
				if n.Type != "taskList" && n.Type != "taskItem" {
					return
				}
				id, _ := n.Attrs["localId"].(string)
				if id == "" || ids[id] {
					t.Errorf("%s has missing or repeated localId %q", n.Type, id)
				}
				ids[id] = true
			})
		})
	}
}

// MCP-7456's shape: a task list holding a nested task list after the item
// it belongs to.
const nestedTasks = `{"type":"doc","version":1,"content":[{"type":"taskList","attrs":{"localId":"l1"},"content":[
	{"type":"taskItem","attrs":{"localId":"i1","state":"TODO"},"content":[{"type":"text","text":"Deploy"}]},
	{"type":"taskList","attrs":{"localId":"l2"},"content":[
		{"type":"taskItem","attrs":{"localId":"i2","state":"DONE"},"content":[{"type":"text","text":"Verify "},{"type":"text","text":"logs","marks":[{"type":"strong"}]}]},
		{"type":"taskItem","attrs":{"localId":"i3","state":"TODO"},"content":[]}]},
	{"type":"taskItem","attrs":{"localId":"i4","state":"DONE"},"content":[{"type":"text","text":"# not a heading"}]}]}]}`

func TestToMarkdownKeepsTaskNesting(t *testing.T) {
	got, err := ToMarkdown(json.RawMessage(nestedTasks))
	if err != nil {
		t.Fatal(err)
	}
	// go-adf writes every nested list as a loose one, after a blank line.
	want := "- [ ] Deploy\n\n  - [x] Verify **logs**\n  - [ ]\n- [x] # not a heading"
	if got != want {
		t.Errorf("ToMarkdown =\n%q\nwant\n%q", got, want)
	}
	doc, err := FromMarkdown(got)
	if err != nil {
		t.Fatal(err)
	}
	wantShape := `doc(taskList(taskItem[TODO](text"Deploy") taskList(taskItem[DONE](text"Verify " text"logs") taskItem[TODO]) taskItem[DONE](text"# not a heading")))`
	if got := shape(parse(t, doc)); got != wantShape {
		t.Errorf("round trip\n got %s\nwant %s", got, wantShape)
	}
}

func TestToMarkdownBlockTaskItem(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[{"type":"taskList","attrs":{"localId":"l"},"content":[
		{"type":"blockTaskItem","attrs":{"localId":"b","state":"DONE"},"content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]},{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}]}]}`
	got, err := ToMarkdown(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	if want := "- [x] first\n\n  second"; got != want {
		t.Errorf("ToMarkdown = %q, want %q", got, want)
	}
}

func TestMediaLinkWithoutNameOrURLIsBare(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"6e7c","type":"file"}}]}]}`
	got, err := ToMarkdown(json.RawMessage(doc), WithMediaLink(func(Media) (string, string) { return "", "" }))
	if err != nil {
		t.Fatal(err)
	}
	if got != "[attachment]" {
		t.Errorf("ToMarkdown = %q, want [attachment]", got)
	}
}
