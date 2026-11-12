package adf

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	goadf "github.com/nyactl/go-adf"
)

// go-adf v0.2.0 reads "- [ ] x" as a bullet whose text starts with "[ ] ",
// and writes a task list's items at column zero whatever their nesting.
// nativeTasks and taskBullets close both gaps, so task lists survive a
// markdown round trip with their nesting.

// nativeTasks turns runs of checkbox bullets into taskList nodes wherever
// ADF allows a task list (everywhere go-adf writes lists except
// blockquotes). An item stays a bullet unless it is one paragraph starting
// with a checkbox, optionally followed by bullet lists that are all tasks.
func nativeTasks(raw json.RawMessage) (json.RawMessage, error) {
	var doc goadf.Node
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("adf: parse converted document: %w", err)
	}
	t := &tasker{}
	doc.Content = t.blocks(doc.Content, true)
	if t.err != nil {
		return nil, t.err
	}
	if !t.changed {
		return raw, nil
	}
	return json.Marshal(doc)
}

type tasker struct {
	changed bool
	err     error
}

// blocks rewrites a block container's children; allowed says whether the
// container may hold a taskList.
func (t *tasker) blocks(nodes []goadf.Node, allowed bool) []goadf.Node {
	out := make([]goadf.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == "bulletList" && allowed {
			out = append(out, t.list(n)...)
			continue
		}
		n.Content = t.blocks(n.Content, n.Type != "blockquote")
		out = append(out, n)
	}
	return out
}

// list splits a bullet list into consecutive task and bullet runs.
func (t *tasker) list(list goadf.Node) []goadf.Node {
	var out []goadf.Node
	var tasks, bullets []goadf.Node
	flush := func() {
		if len(tasks) > 0 {
			out = append(out, goadf.Node{Type: "taskList", Attrs: t.localID(nil), Content: tasks})
			tasks = nil
		}
		if len(bullets) > 0 {
			out = append(out, goadf.Node{Type: "bulletList", Attrs: list.Attrs, Content: bullets})
			bullets = nil
		}
	}
	for _, item := range list.Content {
		if children, ok := t.taskChildren(item); ok {
			if len(bullets) > 0 {
				flush()
			}
			tasks = append(tasks, children...)
			t.changed = true
			continue
		}
		if len(tasks) > 0 {
			flush()
		}
		item.Content = t.blocks(item.Content, true)
		bullets = append(bullets, item)
	}
	flush()
	return out
}

// taskChildren is what a checkbox bullet becomes inside a taskList: its
// taskItem, then a nested taskList for each nested list of tasks.
func (t *tasker) taskChildren(item goadf.Node) ([]goadf.Node, bool) {
	if item.Type != "listItem" || len(item.Content) == 0 || item.Content[0].Type != "paragraph" {
		return nil, false
	}
	state, inline, ok := checkbox(item.Content[0].Content)
	if !ok {
		return nil, false
	}
	var nested [][]goadf.Node
	for _, child := range item.Content[1:] {
		if child.Type != "bulletList" {
			return nil, false
		}
		var sub []goadf.Node
		for _, subItem := range child.Content {
			children, ok := t.taskChildren(subItem)
			if !ok {
				return nil, false
			}
			sub = append(sub, children...)
		}
		nested = append(nested, sub)
	}
	out := []goadf.Node{{Type: "taskItem", Attrs: t.localID(map[string]any{"state": state}), Content: inline}}
	for _, sub := range nested {
		out = append(out, goadf.Node{Type: "taskList", Attrs: t.localID(nil), Content: sub})
	}
	return out, true
}

// checkbox reports whether inline content starts with go-adf's checkbox
// text and returns the task state and the content after it.
func checkbox(inline []goadf.Node) (string, []goadf.Node, bool) {
	if len(inline) == 0 || inline[0].Type != "text" || len(inline[0].Marks) > 0 {
		return "", nil, false
	}
	first := inline[0]
	var state, rest string
	switch {
	case strings.HasPrefix(first.Text, "[ ] "):
		state, rest = "TODO", first.Text[4:]
	case strings.HasPrefix(first.Text, "[x] "), strings.HasPrefix(first.Text, "[X] "):
		state, rest = "DONE", first.Text[4:]
	default:
		return "", nil, false
	}
	out := make([]goadf.Node, 0, len(inline))
	if rest != "" {
		first.Text = rest
		out = append(out, first)
	}
	return state, append(out, inline[1:]...), true
}

func (t *tasker) localID(attrs map[string]any) map[string]any {
	if attrs == nil {
		attrs = map[string]any{}
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil && t.err == nil {
		t.err = fmt.Errorf("adf: task local ID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	attrs["localId"] = h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	return attrs
}

// taskBullets renders a task list as a bullet list whose items start with a
// checkbox token, so nested task lists come out indented under their parent
// item.
func (p *preparer) taskBullets(n goadf.Node) goadf.Node {
	list := goadf.Node{Type: "bulletList"}
	appendItem := func(content ...goadf.Node) {
		list.Content = append(list.Content, goadf.Node{Type: "listItem", Content: content})
	}
	for _, child := range n.Content {
		switch child.Type {
		case "taskItem":
			box := p.checkboxToken(child)
			appendItem(paragraph(append([]goadf.Node{box}, p.prepareChildren(child)...)...))
		case "blockTaskItem":
			box := p.checkboxToken(child)
			blocks := p.prepareChildren(goadf.Node{Type: "doc", Content: child.Content})
			if len(blocks) > 0 && blocks[0].Type == "paragraph" {
				blocks[0].Content = append([]goadf.Node{box}, blocks[0].Content...)
			} else {
				blocks = append([]goadf.Node{paragraph(box)}, blocks...)
			}
			appendItem(blocks...)
		case "taskList":
			nested := p.taskBullets(child)
			if last := len(list.Content) - 1; last >= 0 {
				list.Content[last].Content = append(list.Content[last].Content, nested)
			} else {
				appendItem(nested)
			}
		default:
			appendItem(p.prepare(child, false)...)
		}
	}
	return list
}

func (p *preparer) checkboxToken(n goadf.Node) goadf.Node {
	box := "[ ]"
	if attr(n, "state") == "DONE" {
		box = "[x]"
	}
	token := p.placeholderFor(box)
	token.Text += " "
	return token
}
