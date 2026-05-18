package bitbucket

import "testing"

func TestSnippetListPath(t *testing.T) {
	t.Parallel()

	got := snippetListPath("team space", &SnippetListOptions{
		Limit: 25,
		Role:  "owner",
	})
	want := "/snippets/team%20space?pagelen=25&role=owner"
	if got != want {
		t.Fatalf("snippetListPath() = %q, want %q", got, want)
	}
}

func TestSnippetListPathOmitsEmptyOptions(t *testing.T) {
	t.Parallel()

	got := snippetListPath("team", nil)
	want := "/snippets/team"
	if got != want {
		t.Fatalf("snippetListPath() = %q, want %q", got, want)
	}
}

func TestEscapePathPreservingSlashes(t *testing.T) {
	t.Parallel()

	got, err := escapePathPreservingSlashes("src/auth helpers.go")
	if err != nil {
		t.Fatalf("escapePathPreservingSlashes() error = %v", err)
	}
	want := "src/auth%20helpers.go"
	if got != want {
		t.Fatalf("escapePathPreservingSlashes() = %q, want %q", got, want)
	}
}

func TestEscapePathPreservingSlashesRejectsDotSegments(t *testing.T) {
	t.Parallel()

	for _, filename := range []string{"../config", "src/../config", "./config", "/config", "src//config"} {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()

			if _, err := escapePathPreservingSlashes(filename); err == nil {
				t.Fatalf("escapePathPreservingSlashes(%q) error = nil, want error", filename)
			}
		})
	}
}
