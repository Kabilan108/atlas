package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

// prGoldenCase runs one `atlas pr` command against the prtest fixture. Its
// golden file records stdout, every Bitbucket request with its body, and the
// error, so moving a command onto ops cannot change what it prints or sends.
type prGoldenCase struct {
	name string
	args []string
	// git runs the command in a fresh repository on feature/login whose
	// origin is acme/widgets.
	git  bool
	env  map[string]string
	fail [][]string
}

const goldenSpec = `[{"body":"inline","path":"src/login.go","line":12},{"body":"old side","path":"src/login.go","line":8,"side":"OLD"},{"body":"file note","path":"README.md"},{"body":"reply","reply_to":101}]`

var prGoldenCases = []prGoldenCase{
	{name: "list", args: []string{"list", "-R", "acme/widgets"}},
	{name: "list_json", args: []string{"list", "-R", "acme/widgets", "--json"}},
	{name: "list_author", args: []string{"list", "-R", "acme/widgets", "--author", prtest.GraceID}},
	{name: "list_reviewer_json", args: []string{"list", "-R", "acme/widgets", "--reviewer", "{linus-uuid}", "--json"}},
	{name: "list_merged_empty", args: []string{"list", "-R", "acme/widgets", "--state", "merged"}},
	{name: "list_closed_json", args: []string{"list", "-R", "acme/widgets", "--state", "Closed", "--json"}},
	{name: "list_bad_state", args: []string{"list", "-R", "acme/widgets", "--state", "done"}},
	{name: "list_all", args: []string{"list", "--all"}, env: map[string]string{"ATLAS_WORKSPACE": "acme"}},
	{name: "list_all_repo_flag_json", args: []string{"list", "--all", "-R", "acme/ignored", "--json"}},
	{name: "list_all_no_workspace", args: []string{"list", "--all"}},
	{name: "list_inferred", args: []string{"list"}, git: true},

	{name: "diff_patch", args: []string{"diff", "42", "-R", "acme/widgets", "--patch"}},
	{name: "diff_pipe", args: []string{"diff", "42", "-R", "acme/widgets"}},
	{name: "diff_name_only", args: []string{"diff", "42", "-R", "acme/widgets", "--name-only"}},
	{name: "diff_structured_no_checkout", args: []string{"diff", "42", "-R", "acme/widgets", "-s"}},
	{name: "diff_url", args: []string{"diff", prtest.URL, "--patch"}},

	{name: "status", args: []string{"status", "-R", "acme/widgets"}, git: true},
	{name: "status_json", args: []string{"status", "-R", "acme/widgets", "--json"}, git: true},
	{name: "status_no_git_json", args: []string{"status", "-R", "acme/widgets", "--json"}},

	{name: "create", args: []string{"create", "-R", "acme/widgets", "--title", "Add login", "-b", "Checklist:\n- tests\n\nhttps://bitbucket.org/acme/widgets/pull-requests/42"}, git: true},
	{name: "create_dry_run", args: []string{"create", "-R", "acme/widgets", "--title", "Add login", "-F", "$BODY", "-r", prtest.LinusID + ", @" + prtest.KenID, "--draft", "--dry-run"}, git: true},
	{name: "create_fill_dry_run", args: []string{"create", "-R", "acme/widgets", "--fill", "--dry-run"}, git: true},
	{name: "create_base_head", args: []string{"create", "-R", "acme/widgets", "-B", "release", "-H", "feature/login", "-t", "Backport"}, git: true},
	{name: "create_no_title", args: []string{"create", "-R", "acme/widgets"}, git: true},
	{name: "create_unpushed", args: []string{"create", "-R", "acme/widgets", "-H", "local-only", "-t", "x"}, git: true},
	{name: "create_push_dry_run", args: []string{"create", "-R", "acme/widgets", "-H", "local-only", "-t", "x", "--push", "--dry-run"}, git: true},
	{name: "create_draft_web", args: []string{"create", "--draft", "--web"}},
	{name: "create_body_and_file", args: []string{"create", "-b", "x", "-F", "$BODY"}},
	{name: "create_editor_and_body", args: []string{"create", "-e", "-b", "x"}},
	{name: "create_bad_reviewer", args: []string{"create", "-R", "acme/widgets", "-t", "x", "-r", "nobody"}, git: true},

	{name: "edit_title", args: []string{"edit", "42", "-R", "acme/widgets", "--title", "New title"}},
	{name: "edit_same_title", args: []string{"edit", "42", "-R", "acme/widgets", "--title", "Fix login: handle 'empty' users"}},
	{name: "edit_body_file", args: []string{"edit", "42", "-R", "acme/widgets", "-F", "$BODY"}},
	{name: "edit_clear_body", args: []string{"edit", "42", "-R", "acme/widgets", "--body", ""}},
	{name: "edit_reviewers", args: []string{"edit", "42", "-R", "acme/widgets", "--add-reviewer", "557058:mentioned, " + prtest.GraceID, "--remove-reviewer", "{ken-uuid}"}},
	{name: "edit_draft", args: []string{"edit", "42", "-R", "acme/widgets", "--draft"}},
	{name: "edit_ready", args: []string{"edit", "42", "-R", "acme/widgets", "--ready"}},
	{name: "edit_editor_no_tty", args: []string{"edit", "42", "-R", "acme/widgets"}},
	{name: "edit_body_and_file", args: []string{"edit", "42", "-R", "acme/widgets", "-b", "x", "-F", "$BODY"}},
	{name: "edit_url", args: []string{"edit", prtest.URL, "-t", "Renamed"}},

	{name: "close", args: []string{"close", "42", "-R", "acme/widgets"}},
	{name: "close_comment", args: []string{"close", "42", "-R", "acme/widgets", "-c", "Superseded by #44"}},
	{name: "close_comment_decline_fails", args: []string{"close", "42", "-R", "acme/widgets", "-c", "bye"}, fail: [][]string{{"POST", "/repositories/acme/widgets/pullrequests/42/decline"}}},
	{name: "close_no_arg", args: []string{"close", "-R", "acme/widgets"}},

	{name: "comment", args: []string{"comment", "42", "-R", "acme/widgets", "-b", "hello"}},
	{name: "comment_json", args: []string{"comment", "42", "-R", "acme/widgets", "-b", "hello", "--json"}},
	{name: "comment_body_file", args: []string{"comment", "42", "-R", "acme/widgets", "-F", "$BODY"}},
	{name: "comment_reply", args: []string{"comment", "42", "-R", "acme/widgets", "--reply-to", "101", "-b", "thanks"}},
	{name: "comment_inline_old", args: []string{"comment", "42", "-R", "acme/widgets", "--path", "src/login.go", "--line", "8", "--side", "old", "-b", "why?"}},
	{name: "comment_inline_new", args: []string{"comment", "42", "-R", "acme/widgets", "--path", "src/login.go", "--line", "12", "-b", "nice"}},
	{name: "comment_file", args: []string{"comment", "42", "-R", "acme/widgets", "--path", "README.md", "-b", "typo"}},
	{name: "comment_current_branch", args: []string{"comment", "-R", "acme/widgets", "-b", "branch"}, git: true},
	{name: "comment_missing_body", args: []string{"comment", "42", "-R", "acme/widgets"}},
	{name: "comment_empty_body", args: []string{"comment", "42", "-R", "acme/widgets", "-b", "  "}},
	{name: "comment_two_bodies", args: []string{"comment", "42", "-R", "acme/widgets", "-b", "x", "-e"}},
	{name: "comment_reply_with_path", args: []string{"comment", "42", "-R", "acme/widgets", "--reply-to", "101", "--path", "a.go", "-b", "x"}},
	{name: "comment_line_without_path", args: []string{"comment", "42", "-R", "acme/widgets", "--line", "3", "-b", "x"}},
	{name: "comment_side_without_line", args: []string{"comment", "42", "-R", "acme/widgets", "--path", "a.go", "--side", "old", "-b", "x"}},
	{name: "comment_bad_side", args: []string{"comment", "42", "-R", "acme/widgets", "--path", "a.go", "--line", "3", "--side", "left", "-b", "x"}},
	{name: "comment_negative_reply", args: []string{"comment", "42", "-R", "acme/widgets", "--reply-to", "-1", "-b", "x"}},

	{name: "approve", args: []string{"approve", "42", "-R", "acme/widgets"}},
	{name: "approve_comment_json", args: []string{"approve", "42", "-R", "acme/widgets", "-b", "lgtm", "--json"}},
	{name: "approve_fails_after_comment", args: []string{"approve", "42", "-R", "acme/widgets", "-b", "lgtm"}, fail: [][]string{{"POST", "/repositories/acme/widgets/pullrequests/42/approve"}}},
	{name: "approve_empty_body", args: []string{"approve", "42", "-R", "acme/widgets", "-b", ""}},
	{name: "request_changes", args: []string{"request-changes", "42", "-R", "acme/widgets", "-b", "please fix"}},
	{name: "request_changes_json", args: []string{"request-changes", "42", "-R", "acme/widgets", "--json"}},
	{name: "unapprove", args: []string{"unapprove", "42", "-R", "acme/widgets"}},
	{name: "unapprove_json", args: []string{"unapprove", "42", "-R", "acme/widgets", "--json"}},
	{name: "clear_change_request", args: []string{"clear-change-request", "42", "-R", "acme/widgets"}},
	{name: "clear_change_request_json", args: []string{"clear-change-request", "42", "-R", "acme/widgets", "--json"}},

	{name: "comment_edit", args: []string{"comment", "42", "-R", "acme/widgets", "--edit", "101", "-b", "Handle the error."}},
	{name: "comment_edit_json", args: []string{"comment", prtest.URL, "--edit", "101", "-F", "$BODY", "--json"}},
	{name: "comment_delete", args: []string{"comment", "42", "-R", "acme/widgets", "--delete", "101"}},
	{name: "comment_delete_json", args: []string{"comment", prtest.URL, "--delete", "101", "--json"}},
	{name: "comment_delete_with_body", args: []string{"comment", "42", "-R", "acme/widgets", "--delete", "101", "-b", "x"}},
	{name: "comment_edit_and_delete", args: []string{"comment", "42", "-R", "acme/widgets", "--edit", "1", "--delete", "2"}},
	{name: "edit_base", args: []string{"edit", "42", "-R", "acme/widgets", "--base", "release"}},
	{name: "create_json", args: []string{"create", "-R", "acme/widgets", "-t", "Add login", "--json"}, git: true},

	{name: "review_approve", args: []string{"review", "42", "-R", "acme/widgets", "--approve", "-b", "summary", "--comment-spec", "$SPEC"}},
	{name: "review_comment_json", args: []string{"review", "42", "-R", "acme/widgets", "--comment", "--comment-spec", "$SPEC", "--json"}},
	{name: "review_request_changes_json", args: []string{"review", "42", "-R", "acme/widgets", "--request-changes", "-b", "needs work", "--json"}},
	{name: "review_comment_summary_only", args: []string{"review", "42", "-R", "acme/widgets", "--comment", "-b", "just a note"}},
	{name: "review_approve_only", args: []string{"review", "42", "-R", "acme/widgets", "--approve"}},
	{name: "review_no_action", args: []string{"review", "42", "-R", "acme/widgets", "-b", "x"}},
	{name: "review_two_actions", args: []string{"review", "42", "-R", "acme/widgets", "--approve", "--comment", "-b", "x"}},
	{name: "review_comment_nothing", args: []string{"review", "42", "-R", "acme/widgets", "--comment"}},
	{name: "review_bad_spec", args: []string{"review", "42", "-R", "acme/widgets", "--approve", "--comment-spec", "$BADSPEC"}},
	{name: "review_unparsable_spec", args: []string{"review", "42", "-R", "acme/widgets", "--approve", "--comment-spec", "$BODY"}},
	{name: "review_comment_fails", args: []string{"review", "42", "-R", "acme/widgets", "--approve", "-b", "summary", "--comment-spec", "$SPEC"}, fail: [][]string{{"POST", "/repositories/acme/widgets/pullrequests/42/comments", "1"}}},
	{name: "review_action_fails_json", args: []string{"review", "42", "-R", "acme/widgets", "--request-changes", "--comment-spec", "$SPEC", "--json"}, fail: [][]string{{"POST", "/repositories/acme/widgets/pullrequests/42/request-changes"}}},
}

func TestPRCommandsMatchGolden(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "pr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range prGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got := runPRGolden(t, tc)
			path := filepath.Join(dir, tc.name+".golden")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
				t.Fatalf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

func runPRGolden(t *testing.T, tc prGoldenCase) string {
	t.Helper()
	setupCLIEnv(t)
	t.Setenv("EDITOR", "/atlas-test-editor-must-not-run")
	for key, value := range tc.env {
		t.Setenv(key, value)
	}
	files := t.TempDir()
	placeholders := map[string]string{
		"$SPEC":    goldenSpec,
		"$BADSPEC": `[{"body":"ok"},{"body":"bad","path":"a.go","side":"old"}]`,
		"$BODY":    "Body from file:\n- one\n",
	}
	replacer := []string{}
	for name, content := range placeholders {
		file := filepath.Join(files, strings.TrimPrefix(name, "$")+".txt")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		replacer = append(replacer, name, file)
	}
	args := make([]string, len(tc.args))
	for i, arg := range tc.args {
		args[i] = strings.NewReplacer(replacer...).Replace(arg)
	}
	if tc.git {
		initGoldenGitRepo(t)
	} else {
		t.Chdir(t.TempDir())
	}

	fixture := prtest.New()
	for _, fail := range tc.fail {
		after := 0
		if len(fail) > 2 {
			fmt.Sscan(fail[2], &after)
		}
		fixture.Fail(fail[0], fail[1], after)
	}
	stdout, err := captureStdout(t, func() error {
		root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: fixture}))
		root.PersistentPreRun = nil
		root.SilenceUsage = true
		root.SilenceErrors = true
		root.SetArgs(append([]string{"--no-cache", "pr"}, args...))
		return root.Execute()
	})

	var b strings.Builder
	fmt.Fprintf(&b, "$ atlas pr %s\n--- stdout\n%s", strings.Join(quoteArgs(tc.args), " "), stdout)
	b.WriteString("--- requests\n")
	for _, req := range fixture.Requests() {
		fmt.Fprintf(&b, "%s %s\n", req.Method, req.Path)
		if req.Body != "" {
			fmt.Fprintf(&b, "  %s\n", req.Body)
		}
	}
	if err != nil {
		fmt.Fprintf(&b, "--- error\n%s\n", strings.ReplaceAll(err.Error(), files, "$TMP"))
	}
	return b.String()
}

func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \n'\"") {
			arg = fmt.Sprintf("%q", arg)
		}
		quoted[i] = arg
	}
	return quoted
}

// captureStdout runs fn with os.Stdout sent to a file, since legacy commands
// wrote there directly rather than to the command's writer.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	runErr := fn()
	os.Stdout = original
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

// initGoldenGitRepo makes a repository on feature/login with origin
// acme/widgets, where origin/main is the first commit and origin/feature/login
// the second, so create can fill from commits and see a pushed head.
func initGoldenGitRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	commit := []string{"-c", "user.name=Test", "-c", "user.email=test@example.org", "commit", "-q", "--allow-empty"}
	for _, args := range [][]string{
		{"init", "-q", "-b", "feature/login"},
		{"remote", "add", "origin", "git@bitbucket.org:acme/widgets.git"},
		append(commit, "-m", "Initial"),
		{"update-ref", "refs/remotes/origin/main", "HEAD"},
		append(commit, "-m", "Handle empty users", "-m", "Login rejects an empty user name."),
		{"update-ref", "refs/remotes/origin/feature/login", "HEAD"},
		{"branch", "local-only"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
}
