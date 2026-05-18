package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestParseSnippetRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		raw               string
		fallbackWorkspace string
		want              snippetRef
	}{
		{
			name:              "id with fallback workspace",
			raw:               "abc123",
			fallbackWorkspace: "team",
			want:              snippetRef{Workspace: "team", ID: "abc123"},
		},
		{
			name: "html url",
			raw:  "https://bitbucket.org/snippets/team/abc123/example-title",
			want: snippetRef{Workspace: "team", ID: "abc123"},
		},
		{
			name: "workspace web url",
			raw:  "https://bitbucket.org/moberg-analytics/workspace/snippets/rqMxyL",
			want: snippetRef{Workspace: "moberg-analytics", ID: "rqMxyL"},
		},
		{
			name: "api url",
			raw:  "https://api.bitbucket.org/2.0/snippets/team/abc123",
			want: snippetRef{Workspace: "team", ID: "abc123"},
		},
		{
			name: "clone url trims git suffix",
			raw:  "https://bitbucket.org/snippets/team/abc123.git",
			want: snippetRef{Workspace: "team", ID: "abc123"},
		},
		{
			name: "encoded url parts are unescaped",
			raw:  "https://bitbucket.org/snippets/team%20space/abc%20123",
			want: snippetRef{Workspace: "team space", ID: "abc 123"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSnippetRef(tt.raw, tt.fallbackWorkspace)
			if err != nil {
				t.Fatalf("parseSnippetRef() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseSnippetRef() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseSnippetRefRequiresWorkspaceForID(t *testing.T) {
	t.Parallel()

	if _, err := parseSnippetRef("abc123", ""); err == nil {
		t.Fatal("parseSnippetRef() error = nil, want workspace error")
	}
}

func TestParseSnippetRefRejectsUnsupportedURL(t *testing.T) {
	t.Parallel()

	if _, err := parseSnippetRef("https://example.com/snippets/team/abc123", "team"); err == nil {
		t.Fatal("parseSnippetRef() error = nil, want unsupported URL error")
	}
}

func TestSnippetCloneURL(t *testing.T) {
	t.Parallel()

	ref := snippetRef{Workspace: "team", ID: "abc123"}

	tests := []struct {
		protocol string
		want     string
	}{
		{
			protocol: "ssh",
			want:     "git@bitbucket.org:snippets/team/abc123.git",
		},
		{
			protocol: "https",
			want:     "https://bitbucket.org/snippets/team/abc123.git",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.protocol, func(t *testing.T) {
			t.Parallel()

			got, err := snippetCloneURL(ref, tt.protocol)
			if err != nil {
				t.Fatalf("snippetCloneURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("snippetCloneURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSnippetCloneURLRejectsInvalidProtocol(t *testing.T) {
	t.Parallel()

	_, err := snippetCloneURL(snippetRef{Workspace: "team", ID: "abc123"}, "ftp")
	if err == nil {
		t.Fatal("snippetCloneURL() error = nil, want invalid protocol error")
	}
}

func TestSnippetLocalFilename(t *testing.T) {
	t.Parallel()

	if got := snippetLocalFilename("src/auth.go"); got != "src/auth.go" {
		t.Fatalf("snippetLocalFilename() = %q, want %q", got, "src/auth.go")
	}
}

func TestLimitSnippetsAfterVisibilityFiltering(t *testing.T) {
	t.Parallel()

	snippets := []bitbucket.Snippet{
		{ID: "1", IsPrivate: false},
		{ID: "2", IsPrivate: true},
		{ID: "3", IsPrivate: true},
		{ID: "4", IsPrivate: true},
	}

	filtered := filterSnippetsByVisibility(snippets, false, true)
	got := limitSnippets(filtered, 2)

	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "3" {
		t.Fatalf("filtered limited snippets = %#v, want IDs 2 and 3", got)
	}
}

func TestSanitizeTerminalContent(t *testing.T) {
	t.Parallel()

	got := sanitizeTerminalContent("ok\x1b[31m\n\t")
	want := "ok\\x1b[31m\n\t"
	if got != want {
		t.Fatalf("sanitizeTerminalContent() = %q, want %q", got, want)
	}
}

func TestRequireSnippetFileRejectsMissingFile(t *testing.T) {
	t.Parallel()

	snippet := &bitbucket.Snippet{
		Files: map[string]bitbucket.SnippetFile{
			"exists.go": {},
		},
	}

	if err := requireSnippetFile(snippet, "abc123", "typo.go"); err == nil {
		t.Fatal("requireSnippetFile() error = nil, want missing file error")
	}
}

func TestEditBufferSupportsQuotedEditorCommand(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "edit script.sh")
	script := "#!/bin/sh\nprintf changed > \"$1\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("failed to write editor script: %v", err)
	}

	t.Setenv("VISUAL", "'"+scriptPath+"'")
	t.Setenv("EDITOR", "")

	got, err := editBuffer("snippet.txt", []byte("original"))
	if err != nil {
		t.Fatalf("editBuffer() error = %v", err)
	}
	if string(got) != "changed" {
		t.Fatalf("editBuffer() = %q, want %q", got, "changed")
	}
}
