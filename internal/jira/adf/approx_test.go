package adf

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApproximate(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"empty", ``, ""},
		{"null", `null`, ""},
		{"plain text field", `"just text"`, ""},
		{"agent markdown has none", "", ""},
		{
			name: "media and wrappers count once per file",
			doc:  `{"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"a"}},{"type":"caption","content":[{"type":"text","text":"c"}]}]},{"type":"mediaGroup","content":[{"type":"media","attrs":{"id":"b"}}]}]}`,
			want: "media x2",
		},
		{
			name: "marks",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"u","marks":[{"type":"underline"}]},{"type":"text","text":"c","marks":[{"type":"textColor","attrs":{"color":"#f00"}},{"type":"strong"}]},{"type":"text","text":"b","marks":[{"type":"backgroundColor"}]}]}]}`,
			want: "text color x2, underline x1",
		},
		{
			name: "expand, cards, panel and unknown nodes",
			doc:  `{"type":"doc","content":[{"type":"expand","attrs":{"title":"t"},"content":[{"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://x"}}]}]},{"type":"panel","content":[{"type":"paragraph"}]},{"type":"futureWidget"}]}`,
			want: "expand x1, panel x1, smart link x1, unsupported node x1",
		},
		{
			name: "simple table",
			doc:  `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph"}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}]}`,
			want: "",
		},
		{
			name: "headerless table",
			doc:  `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}]}`,
			want: "complex table x1",
		},
		{
			name: "ragged table",
			doc:  `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader"},{"type":"tableHeader"}]},{"type":"tableRow","content":[{"type":"tableCell"}]}]}]}`,
			want: "complex table x1",
		},
		{
			name: "merged cell",
			doc:  `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":2}}]}]}]}`,
			want: "complex table x1",
		},
		{
			name: "multi-paragraph cell",
			doc:  `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph"},{"type":"paragraph"}]}]}]}]}`,
			want: "complex table x1",
		},
		{"nested tasks round-trip now", nestedTasks, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := json.RawMessage(tc.doc)
			if tc.name == "agent markdown has none" {
				var err error
				if doc, err = FromMarkdown("## H\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\n- [ ] t\n  - [x] u\n\n> q\n\n```go\nx\n```\n\n**b** *i* [l](https://x)"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Approximate(doc)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("Approximate = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestApproximationsAddAndTotal(t *testing.T) {
	a := Approximations{"media": 2}
	a.Add(Approximations{"media": 1, "expand": 1})
	if a.Total() != 4 || a.String() != "expand x1, media x3" {
		t.Errorf("got %v (total %d)", a, a.Total())
	}
}

func TestAppendKeepsExistingBlocksVerbatim(t *testing.T) {
	existing := `{"type":"doc","version":1,"content":[{"type":"mediaSingle","attrs":{"layout":"center"},"content":[{"type":"media","attrs":{"id":"m","type":"file","collection":"c"}}]},{"type":"paragraph","content":[{"type":"text","text":"a < b & c","marks":[{"type":"textColor","attrs":{"color":"#ff5630"}}]}]}]}`
	addition, err := FromMarkdown("- [ ] follow up")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Append(json.RawMessage(existing), addition)
	if err != nil {
		t.Fatal(err)
	}
	_, blocks, _ := strings.Cut(strings.TrimSuffix(existing, "]}"), `"content":[`)
	if !strings.Contains(string(got), `"content":[`+blocks+`,{"type":"taskList"`) {
		t.Errorf("existing blocks changed:\n%s", got)
	}
	if !strings.Contains(string(got), `"type":"taskList"`) {
		t.Errorf("addition missing: %s", got)
	}
	if !json.Valid(got) {
		t.Errorf("invalid JSON: %s", got)
	}
}

func TestAppendEdgeCases(t *testing.T) {
	addition := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`)
	for _, existing := range []string{"", "null", " "} {
		got, err := Append(json.RawMessage(existing), addition)
		if err != nil || string(got) != string(addition) {
			t.Errorf("Append(%q) = %s, %v", existing, got, err)
		}
	}
	got, err := Append(json.RawMessage(`{"type":"doc","version":1}`), addition)
	if err != nil || !strings.Contains(string(got), `"content":[{"type":"paragraph"}]`) {
		t.Errorf("Append to a doc without content = %s, %v", got, err)
	}
	if _, err := Append(json.RawMessage(`"text"`), addition); err == nil {
		t.Error("Append to a string = nil error")
	}
	if _, err := Append(json.RawMessage(`{"type":"paragraph"}`), addition); err == nil {
		t.Error("Append to a non-doc = nil error")
	}
}
