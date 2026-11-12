package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/snippet/snippettest"
)

const snippetGoldenDir = "testdata/snippet"

// snippetGoldenCases were recorded from the snippet commands before they
// moved onto ops; the moved commands must print the same bytes and make the
// same Bitbucket calls.
var snippetGoldenCases = []struct {
	name  string
	args  []string
	stdin string
}{
	{"list", []string{"list", "--workspace", "acme"}, ""},
	{"list_json", []string{"list", "--workspace", "acme", "--json"}, ""},
	{"list_private_limit", []string{"list", "--workspace", "acme", "--private", "-L", "1"}, ""},
	{"list_role", []string{"ls", "--workspace", "acme", "--role", "owner"}, ""},
	{"list_empty", []string{"list", "--workspace", "empty"}, ""},
	{"list_both_visibilities", []string{"list", "--workspace", "acme", "--public", "--private"}, ""},
	{"list_bad_role", []string{"list", "--workspace", "acme", "--role", "admin"}, ""},
	{"view", []string{"view", "abc123", "--workspace", "acme"}, ""},
	{"view_raw_file", []string{"view", "abc123", "--workspace", "acme", "-r", "-f", "run.sh"}, ""},
	{"view_missing_file", []string{"view", "abc123", "--workspace", "acme", "-f", "nope.txt"}, ""},
	{"view_files", []string{"view", "abc123", "--workspace", "acme", "--files"}, ""},
	{"view_json", []string{"view", "abc123", "--workspace", "acme", "--json"}, ""},
	{"view_json_files", []string{"view", "abc123", "--workspace", "acme", "--json", "--files"}, ""},
	{"view_url", []string{"view", "https://bitbucket.org/snippets/acme/def456"}, ""},
	{"view_no_arg", []string{"view", "--workspace", "acme"}, ""},
	{"view_not_found", []string{"view", "zzz999", "--workspace", "acme"}, ""},
	{"create", []string{"create", "a.txt", "dir/b.txt", "--title", "Two files", "--workspace", "acme"}, ""},
	{"create_json_public", []string{"create", "a.txt", "--title", "Public", "-p", "--workspace", "acme", "--json"}, ""},
	{"create_stdin", []string{"new", "-", "-f", "piped.txt", "--title", "Piped", "--workspace", "acme"}, "from stdin\n"},
	{"create_stdin_implicit", []string{"create", "--title", "Implicit", "--workspace", "acme"}, "implicit\n"},
	{"create_duplicate", []string{"create", "a.txt", "./a.txt", "--title", "Dup", "--workspace", "acme"}, ""},
	{"edit_title", []string{"edit", "abc123", "--title", "Renamed", "--workspace", "acme"}, ""},
	{"edit_add", []string{"edit", "abc123", "--add", "a.txt", "--workspace", "acme"}, ""},
	{"edit_add_as", []string{"edit", "abc123", "renamed.txt", "--add", "a.txt", "--workspace", "acme"}, ""},
	{"edit_remove", []string{"edit", "https://bitbucket.org/snippets/acme/abc123", "-r", "run.sh"}, ""},
	{"edit_editor", []string{"edit", "abc123", "notes.md", "--workspace", "acme"}, ""},
	{"edit_editor_ambiguous", []string{"edit", "abc123", "--workspace", "acme"}, ""},
	{"edit_editor_single", []string{"edit", "def456", "--workspace", "acme"}, ""},
	{"edit_editor_missing", []string{"edit", "abc123", "typo.md", "--workspace", "acme"}, ""},
	{"edit_title_with_filename", []string{"edit", "abc123", "notes.md", "--title", "X", "--workspace", "acme"}, ""},
	{"rename", []string{"rename", "abc123", "run.sh", "deploy.sh", "--workspace", "acme"}, ""},
	{"delete", []string{"delete", "abc123", "--yes", "--workspace", "acme"}, ""},
	{"delete_unconfirmed", []string{"delete", "abc123", "--workspace", "acme"}, ""},
}

func TestSnippetCommandsMatchGolden(t *testing.T) {
	goldenDir, err := filepath.Abs(snippetGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range snippetGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got := runSnippetGolden(t, tc.args, tc.stdin)
			path := filepath.Join(goldenDir, tc.name+".txt")
			if *updateGolden {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("atlas snippet %v differs from %s\n--- got ---\n%s\n--- want ---\n%s", tc.args, path, got, want)
			}
		})
	}
}

// runSnippetGolden runs one snippet command in a scratch directory holding
// a.txt and dir/b.txt, with stdin from a file and an editor that appends a
// line, and returns its stdout, error, and Bitbucket requests.
func runSnippetGolden(t *testing.T, args []string, stdin string) string {
	t.Helper()
	setupCLIEnv(t)
	dir := t.TempDir()
	t.Chdir(dir)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "alpha\n")
	writeTestFile(t, filepath.Join(dir, "dir", "b.txt"), "beta\n")
	editor := filepath.Join(t.TempDir(), "editor.sh")
	writeTestFile(t, editor, "#!/bin/sh\nprintf 'edited\\n' >> \"$1\"\n")
	if err := os.Chmod(editor, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", editor)
	t.Setenv("EDITOR", "")

	stdinPath := filepath.Join(t.TempDir(), "stdin")
	writeTestFile(t, stdinPath, stdin)
	stdinFile, err := os.Open(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	defer stdinFile.Close()
	oldStdin := os.Stdin
	os.Stdin = stdinFile
	defer func() { os.Stdin = oldStdin }()

	fixture := snippettest.New()
	var cmdOut string
	var runErr error
	osOut := captureSnippetStdout(t, func() {
		cmdOut, runErr = runAtlas(t, fixture, append([]string{"--no-cache", "snippet"}, args...)...)
	})

	var b strings.Builder
	b.WriteString("$ atlas snippet " + strings.Join(args, " ") + "\n")
	b.WriteString(osOut + cmdOut)
	b.WriteString("--- error ---\n")
	if runErr != nil {
		b.WriteString(runErr.Error() + "\n")
	}
	b.WriteString("--- requests ---\n")
	for _, req := range fixture.Requests() {
		b.WriteString(req.String() + "\n")
	}
	return b.String()
}

// captureStdout returns what fn writes to os.Stdout.
func captureSnippetStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()
	defer func() { os.Stdout = old }()
	fn()
	writer.Close()
	os.Stdout = old
	return <-done
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
