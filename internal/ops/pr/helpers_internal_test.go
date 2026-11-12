package pr

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestParseState(t *testing.T) {
	for raw, want := range map[string]State{"": StateOpen, "open": StateOpen, "MERGED": StateMerged, " closed ": StateDeclined, "declined": StateDeclined, "Superseded": StateSuperseded} {
		got, err := ParseState(raw)
		if err != nil || got != want {
			t.Errorf("ParseState(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := ParseState("done"); err == nil {
		t.Error("ParseState(done) accepted an unknown state")
	}
}

func TestDiffFileNames(t *testing.T) {
	t.Parallel()

	diff := []byte(`diff --git a/README.md b/README.md
diff --git a/deleted.go /dev/null
diff --git a/internal/cli/pr.go b/internal/cli/pr.go
diff --git a/README.md b/README.md
`)

	want := []string{"README.md", "deleted.go", "internal/cli/pr.go"}
	if got := DiffFiles(diff); !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffFiles() = %#v, want %#v", got, want)
	}
}

func TestNormalizeDescriptionAddsBlankLinesBeforeLists(t *testing.T) {
	t.Parallel()

	body := "Description/Summary of Changes:\n- Add reusable CNS Events.xml annotation import logic.\n- Deduplicate imports by CNS event fingerprint.\n\nTo-Do:\n- [ ] Code review\n- [ ] Address feedback\n\nTesting Steps:\n1. Run `pytest ...`.\n"
	want := "Description/Summary of Changes:\n\n- Add reusable CNS Events.xml annotation import logic.\n- Deduplicate imports by CNS event fingerprint.\n\nTo-Do:\n\n- [ ] Code review\n- [ ] Address feedback\n\nTesting Steps:\n\n1. Run `pytest ...`.\n"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionPreservesExistingBlankLinesAndCodeFences(t *testing.T) {
	t.Parallel()

	body := "Description:\n\n- Already separated\n\n```md\nExample:\n- Not a real list block\n```\n\nChecklist:\n\t- Tab-indented task\n"
	want := "Description:\n\n- Already separated\n\n```md\nExample:\n- Not a real list block\n```\n\nChecklist:\n\n\t- Tab-indented task\n"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionPreservesCRLF(t *testing.T) {
	t.Parallel()

	body := "To-Do:\r\n- [ ] Review\r\n"
	want := "To-Do:\r\n\r\n- [ ] Review\r\n"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionPreservesLongFenceWithShortFenceInside(t *testing.T) {
	t.Parallel()

	body := "````md\n```md\nExample:\n- Not a real list block\n```\nStill fenced:\n- Also not a real list block\n````\nReal section:\n- Real list block\n"
	want := "````md\n```md\nExample:\n- Not a real list block\n```\nStill fenced:\n- Also not a real list block\n````\nReal section:\n\n- Real list block\n"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionAddsInlineCardsToStandaloneURLs(t *testing.T) {
	t.Parallel()

	body := "https://moberganalytics.atlassian.net/browse/MCP-7163\n\nhttps://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361"
	want := "[https://moberganalytics.atlassian.net/browse/MCP-7163](https://moberganalytics.atlassian.net/browse/MCP-7163){: data-inline-card='' }\n\n[https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionAddsInlineCardsToURLListItems(t *testing.T) {
	t.Parallel()

	body := "- https://moberganalytics.atlassian.net/browse/MCP-7163\n  1.   https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361  \n"
	want := "- [https://moberganalytics.atlassian.net/browse/MCP-7163](https://moberganalytics.atlassian.net/browse/MCP-7163){: data-inline-card='' }\n  1.   [https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }  \n"

	if got := NormalizeDescription(body); got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestNormalizeDescriptionSkipsInlineCardIneligibleURLs(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361{: data-inline-card='' }",
		"[https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }",
		"[dashboard PR](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361)",
		"See https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361",
		"https://example.com/moberg-analytics/dashboard/pull-requests/1361",
		"```md",
		"https://moberganalytics.atlassian.net/browse/MCP-7163",
		"```",
		"",
	}, "\n")

	if got := NormalizeDescription(body); got != body {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, body)
	}
}

func TestCommentSpecBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec CommentSpec
		want func(*testing.T, bitbucket.CommentCreate)
	}{
		{
			name: "top level",
			spec: CommentSpec{Body: "Looks good"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Content.Raw != "Looks good" {
					t.Fatalf("Content.Raw = %q, want %q", got.Content.Raw, "Looks good")
				}
				if got.Parent != nil || got.Inline != nil {
					t.Fatalf("Parent = %#v Inline = %#v, want nil", got.Parent, got.Inline)
				}
			},
		},
		{
			name: "reply",
			spec: CommentSpec{Body: "Fixed", ReplyTo: 123},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Parent == nil || got.Parent.ID != 123 {
					t.Fatalf("Parent = %#v, want id 123", got.Parent)
				}
				if got.Inline != nil {
					t.Fatalf("Inline = %#v, want nil", got.Inline)
				}
			},
		},
		{
			name: "file level",
			spec: CommentSpec{Body: "Question", Path: "internal/cli/pr.go"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.Path != "internal/cli/pr.go" {
					t.Fatalf("Inline = %#v, want path", got.Inline)
				}
				if got.Inline.From != nil || got.Inline.To != nil {
					t.Fatalf("Inline = %#v, want no line", got.Inline)
				}
			},
		},
		{
			name: "new line",
			spec: CommentSpec{Body: "Question", Path: "internal/cli/pr.go", Line: 42, Side: "new"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.To == nil || *got.Inline.To != 42 {
					t.Fatalf("Inline = %#v, want to line 42", got.Inline)
				}
				if got.Inline.From != nil {
					t.Fatalf("From = %#v, want nil", got.Inline.From)
				}
			},
		},
		{
			name: "old line",
			spec: CommentSpec{Body: "Question", Path: "internal/cli/pr.go", Line: 42, Side: "old"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.From == nil || *got.Inline.From != 42 {
					t.Fatalf("Inline = %#v, want from line 42", got.Inline)
				}
				if got.Inline.To != nil {
					t.Fatalf("To = %#v, want nil", got.Inline.To)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.spec.build()
			if err != nil {
				t.Fatalf("build() error = %v", err)
			}
			tt.want(t, got)
		})
	}
}

func TestCommentSpecBuildRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec CommentSpec
	}{
		{name: "empty body", spec: CommentSpec{Body: "  "}},
		{name: "line without path", spec: CommentSpec{Body: "body", Line: 3}},
		{name: "side without line", spec: CommentSpec{Body: "body", Path: "file.go", Side: "old"}},
		{name: "invalid side", spec: CommentSpec{Body: "body", Path: "file.go", Line: 3, Side: "right"}},
		{name: "reply with path", spec: CommentSpec{Body: "body", ReplyTo: 1, Path: "file.go"}},
		{name: "reply with line", spec: CommentSpec{Body: "body", ReplyTo: 1, Line: 3}},
		{name: "reply with side", spec: CommentSpec{Body: "body", ReplyTo: 1, Side: "new"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tt.spec.build(); err == nil {
				t.Fatal("build() error = nil, want error")
			}
		})
	}
}

func TestFilterDiffKeepsMatchingFiles(t *testing.T) {
	diff := "diff --git a/a/x.go b/a/x.go\n+x\ndiff --git a/b.go b/b.go\n+b\ndiff --git a/old/y.go b/new/y.go\n+y\n"
	tests := map[string]string{
		"a":         "diff --git a/a/x.go b/a/x.go\n+x\n",
		"b.go":      "diff --git a/b.go b/b.go\n+b\n",
		"old":       "diff --git a/old/y.go b/new/y.go\n+y\n",
		"new/y.go":  "diff --git a/old/y.go b/new/y.go\n+y\n",
		"a/x":       "",
		"elsewhere": "",
	}
	for path, want := range tests {
		if got := string(filterDiff([]byte(diff), path)); got != want {
			t.Errorf("filterDiff(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFilterDiffMatchesExactPaths(t *testing.T) {
	sections := []string{
		"diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n",
		"diff --git a/b/file.go b/b/file.go\n--- a/b/file.go\n+++ b/b/file.go\n@@ -1 +1 @@\n--- a/file.go\n+new\n",
		"diff --git a/pkg/file.go b/pkg/file.go\n--- a/pkg/file.go\n+++ b/pkg/file.go\n@@ -1 +1 @@\n-old\n+new\n",
		"diff --git a/docs/a b.md b/docs/a b.md\nindex 1..2 100644\n--- a/docs/a b.md\t\n+++ b/docs/a b.md\t\n@@ -1 +1 @@\n-old\n+new\n",
		"diff --git a/img/my pic.png b/img/my pic.png\nindex 1..2 100644\nBinary files a/img/my pic.png and b/img/my pic.png differ\n",
		"diff --git \"a/tab\\there.go\" \"b/tab\\there.go\"\n--- \"a/tab\\there.go\"\n+++ \"b/tab\\there.go\"\n@@ -1 +1 @@\n-old\n+new\n",
		"diff --git \"a/caf\\303\\251.md\" \"b/caf\\303\\251.md\"\nnew file mode 100644\n--- /dev/null\n+++ \"b/caf\\303\\251.md\"\n@@ -0,0 +1 @@\n+new\n",
		"diff --git a/old name.go b/new name.go\nsimilarity index 90%\nrename from old name.go\nrename to new name.go\n",
		"diff --git a/a b/c.go b/a b/d.go\nsimilarity index 100%\nrename from a b/c.go\nrename to a b/d.go\n",
		"diff --git a/gone.go b/gone.go\ndeleted file mode 100644\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n",
	}
	diff := strings.Join(sections, "")
	tests := map[string][]int{
		"file.go":        {0},
		"b/file.go":      {1},
		"b":              {1},
		"pkg":            {2},
		"pkg/file.go":    {2},
		"docs/a b.md":    {3},
		"docs":           {3},
		"b.md":           nil,
		"img/my pic.png": {4},
		"tab\there.go":   {5},
		"café.md":        {6},
		"old name.go":    {7},
		"new name.go":    {7},
		"name.go":        nil,
		"a b":            {8},
		"a b/c.go":       {8},
		"a b/d.go":       {8},
		"gone.go":        {9},
		"a/file.go":      nil,
		"doc":            nil,
		"docs/a b.md/x":  nil,
	}
	for path, want := range tests {
		var b strings.Builder
		for _, i := range want {
			b.WriteString(sections[i])
		}
		if got := string(filterDiff([]byte(diff), path)); got != b.String() {
			t.Errorf("filterDiff(%q) = %q, want %q", path, got, b.String())
		}
	}

	wantFiles := []string{"file.go", "b/file.go", "pkg/file.go", "docs/a b.md", "img/my pic.png", "tab\there.go", "café.md", "new name.go", "a b/d.go", "gone.go"}
	if got := DiffFiles([]byte(diff)); !reflect.DeepEqual(got, wantFiles) {
		t.Errorf("DiffFiles() = %#v, want %#v", got, wantFiles)
	}
}
