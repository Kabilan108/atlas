package output

import "testing"

func TestMarkdownTableEscapesCells(t *testing.T) {
	got := MarkdownTable([]string{"A", "B"}, [][]string{{"x|y", "line\nbreak"}})
	want := "| A | B |\n| --- | --- |\n| x\\|y | line break |\n"
	if got != want {
		t.Fatalf("MarkdownTable() = %q, want %q", got, want)
	}
}

func TestCodeFenceOutlastsContentBackticks(t *testing.T) {
	got := CodeFence("a ```` b", "go")
	want := "`````go\na ```` b\n`````\n"
	if got != want {
		t.Fatalf("CodeFence() = %q, want %q", got, want)
	}
}

func TestFenceLanguage(t *testing.T) {
	for name, want := range map[string]string{
		"version.json": "json",
		"ci/build.yml": "yaml",
		"Makefile":     "",
		".env":         "",
		"notes.txt":    "",
		"weird.e-x":    "",
		"src/main.go":  "go",
		"README.MD":    "markdown",
	} {
		if got := FenceLanguage(name); got != want {
			t.Errorf("FenceLanguage(%q) = %q, want %q", name, got, want)
		}
	}
}
