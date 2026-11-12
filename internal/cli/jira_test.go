package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	jiraops "github.com/kabilan108/atlas/internal/ops/jira"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

func setupJiraCLIEnv(t *testing.T) {
	t.Helper()
	setupCLIEnv(t)
	t.Setenv("ATLAS_JIRA_TOKEN", "cli-jira-token")
	t.Setenv("ATLAS_JIRA_CLOUD_ID", "cloud-1")
}

func jiraIssueFake() *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-7", http.StatusOK, `{"id":"7","key":"CSM-7","fields":{
		"summary":"Gateway stuck","status":{"name":"Open"},"labels":["field"],
		"description":{"type":"doc","version":1,"content":[{"type":"taskList","attrs":{"localId":"l"},"content":[
			{"type":"taskItem","attrs":{"localId":"a","state":"TODO"},"content":[{"type":"text","text":"restart"}]}]}]},
		"customfield_1":"2.4.1"},"names":{"customfield_1":"Software Version"},"schema":{"customfield_1":{"type":"string"}}}`)
	fake.JSON("GET /rest/api/3/issue/CSM-7/comment", http.StatusOK, `{"startAt":0,"total":1,"comments":[
		{"id":"1","author":{"displayName":"Mia"},"created":"2026-11-11T09:00:00.000-0500","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]}}]}`)
	return fake
}

// `atlas jira view --raw` and the MCP tool come from one op, so on the same
// data they print the same bytes and make the same Jira calls.
func TestJiraViewParityWithMCP(t *testing.T) {
	setupJiraCLIEnv(t)
	cliFake := jiraIssueFake()
	got, err := runAtlas(t, cliFake.Transport(), "jira", "view", "CSM-7", "--raw", "-c", "3")
	if err != nil {
		t.Fatal(err)
	}
	mcpFake := jiraIssueFake()
	res, err := jiraops.View.Invoke(context.Background(), opstest.New("").WithJira(t, mcpFake), json.RawMessage(`{"issue":"CSM-7","comments":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != res.Text {
		t.Fatalf("CLI and MCP differ\n--- cli ---\n%s\n--- mcp ---\n%s", got, res.Text)
	}
	if strings.Join(cliFake.Routes(), "\n") != strings.Join(mcpFake.Routes(), "\n") {
		t.Errorf("Jira calls differ\ncli: %v\nmcp: %v", cliFake.Routes(), mcpFake.Routes())
	}
	if !strings.Contains(got, "- [ ] restart") || !strings.Contains(got, "- **Software Version:** 2.4.1") {
		t.Errorf("view output:\n%s", got)
	}
	req := cliFake.Requests()[0]
	user, token, _ := (&http.Request{Header: req.Header}).BasicAuth()
	if user != "cli-user" || token != "cli-jira-token" {
		t.Errorf("CLI sent %q / %q, want the configured Jira credentials", user, token)
	}
}

func TestJiraCreateFieldFlags(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes", http.StatusOK, `{"isLast":true,"issueTypes":[{"id":"1","name":"Bug"}]}`)
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes/1", http.StatusOK, `{"isLast":true,"fields":[
		{"fieldId":"customfield_10153","name":"Product","schema":{"type":"array","items":"option"},"allowedValues":[{"value":"A"},{"value":"B"}]},
		{"fieldId":"customfield_10469","name":"Software Version","schema":{"type":"string"}}]}`)
	fake.JSON("POST /rest/api/3/issue", http.StatusCreated, `{"id":"9","key":"CSM-9"}`)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("Details"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := runAtlas(t, fake.Transport(), "jira", "create", "-p", "CSM", "-t", "Bug", "-s", "Stuck", "-F", bodyFile,
		"--field", "Product=A", "--field", "Product=b", "--field", "Software Version=1.2=rc", "-l", "x", "-l", "y")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Created CSM-9: https://acme.atlassian.net/browse/CSM-9\n" {
		t.Errorf("output = %q", got)
	}
	var sent struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal([]byte(fake.Find("POST /rest/api/3/issue")[0].Body), &sent); err != nil {
		t.Fatal(err)
	}
	product, _ := json.Marshal(sent.Fields["customfield_10153"])
	labels, _ := json.Marshal(sent.Fields["labels"])
	if string(product) != `[{"value":"A"},{"value":"B"}]` || sent.Fields["customfield_10469"] != "1.2=rc" || string(labels) != `["x","y"]` {
		t.Errorf("fields = %v", sent.Fields)
	}
}

func TestJiraCreateLinkFlags(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes", http.StatusOK, `{"isLast":true,"issueTypes":[{"id":"1","name":"Bug"}]}`)
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes/1", http.StatusOK, `{"isLast":true,"fields":[]}`)
	fake.JSON("POST /rest/api/3/issue", http.StatusCreated, `{"id":"9","key":"CSM-9"}`)
	fake.Links(`{"issueLinkTypes":[{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"}]}`)
	got, err := runAtlas(t, fake.Transport(), "jira", "create", "-p", "CSM", "-t", "Bug", "-s", "Stuck",
		"--link", "Blocks:CSM-2", "--inward-link", "blocks:CSM-3")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Created CSM-9: https://acme.atlassian.net/browse/CSM-9\nLinked: CSM-9 blocks CSM-2\nLinked: CSM-9 is blocked by CSM-3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if _, err := runAtlas(t, fake.Transport(), "jira", "create", "-p", "CSM", "-t", "Bug", "-s", "Stuck", "--link", "CSM-2"); err == nil || !strings.Contains(err.Error(), "expected TYPE:KEY") {
		t.Fatalf("err = %v, want a TYPE:KEY error", err)
	}
}

func TestJiraFieldFlagParsing(t *testing.T) {
	cmd := newJiraEditCmd()
	if err := cmd.ParseFlags([]string{"--field", `Product=["A","B"]`, "--field", "Product=C", "--field", "Empty=null", "--field", "Version= 2 "}); err != nil {
		t.Fatal(err)
	}
	fields, err := jiraFields(cmd)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(fields)
	if string(data) != `{"Empty":null,"Product":["A","B","C"],"Version":" 2 "}` {
		t.Errorf("fields = %s", data)
	}
	for _, bad := range []string{"NoEquals", "=value", `X=[unterminated`} {
		cmd := newJiraEditCmd()
		_ = cmd.ParseFlags([]string{"--field", bad})
		if _, err := jiraFields(cmd); err == nil {
			t.Errorf("--field %q accepted", bad)
		}
	}
}

func TestJiraWorklogAddEntriesFileAndPartialExit(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me"}`)
	fake.IssueWorklogs("10001", "CSM-1", "acc-me")
	fake.IssueWorklogs("10002", "CSM-2", "acc-me")
	fake.JSON("POST /rest/api/3/issue/10002/worklog", http.StatusBadRequest, `{"errorMessages":["Worklog must not be null."]}`)
	entries := filepath.Join(t.TempDir(), "entries.json")
	if err := os.WriteFile(entries, []byte(`[{"issue":"CSM-1","started":"2026-11-11T09:00:00-05:00","duration":"1h"},
		{"issue":"CSM-2","started":"2026-11-11T10:00:00-05:00","duration":"30m"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := runAtlas(t, fake.Transport(), "jira", "worklog", "add", "-f", entries)
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want Partial", err)
	}
	want := "0 ok CSM-1 2026-11-11T09:00:00.000-0500 1h worklog 900\n1 failed CSM-2 2026-11-11T10:00:00.000-0500 30m: "
	if !strings.HasPrefix(got, want) {
		t.Errorf("output = %q", got)
	}
	if _, err := runAtlas(t, fake.Transport(), "jira", "worklog", "add", "-f", entries, "CSM-1"); err == nil {
		t.Error("worklog add accepted positional args with --entries-file")
	}
}

func TestJiraAttachReadsFiles(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("POST /rest/api/3/issue/CSM-1/attachments", http.StatusOK, `[{"id":"5","filename":"a.txt","size":5}]`)
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newJiraAttachCmd()
	in, err := jiraAttachInput(cmd, []string{"CSM-1", path})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Files) != 1 || in.Files[0].Filename != "a.txt" || in.Files[0].Content != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Errorf("input = %+v", in)
	}
	got, err := runAtlas(t, fake.Transport(), "jira", "attach", "CSM-1", path)
	if err != nil || got != "Attached a.txt (5 bytes) to CSM-1: https://acme.atlassian.net/rest/api/3/attachment/content/5\n" {
		t.Errorf("attach = %q, %v", got, err)
	}
}

func TestJiraAllMapsToTheMaximum(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("POST /rest/api/3/search/jql", http.StatusOK, `{"isLast":true,"issues":[]}`)
	if _, err := runAtlas(t, fake.Transport(), "jira", "search", "project = CSM", "-L", "-1"); err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	_ = json.Unmarshal([]byte(fake.Find("POST /rest/api/3/search/jql")[0].Body), &req)
	if req["maxResults"] != float64(100) {
		t.Errorf("first page = %v", req["maxResults"])
	}
	if allMeansMax(-1, 7) != 7 || allMeansMax(3, 7) != 3 || allMeansMax(0, 7) != 0 {
		t.Error("allMeansMax")
	}
	if _, err := runAtlas(t, fake.Transport(), "jira", "search", "project = CSM", "-L", "-2"); ops.KindOf(err) != ops.Invalid {
		t.Errorf("-L -2: %v", err)
	}
}

func TestJiraCommentNeedsBody(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	if _, err := runAtlas(t, fake.Transport(), "jira", "internal-comment", "CSM-1"); err == nil || !strings.Contains(err.Error(), "body is required") {
		t.Errorf("err = %v", err)
	}
	if len(fake.Requests()) != 0 {
		t.Errorf("requests = %v", fake.Routes())
	}
}

func runAtlasStdin(t *testing.T, fixture http.RoundTripper, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	root := NewRootCmd("test", WithHTTPClient(&http.Client{Transport: fixture}))
	root.PersistentPreRun = nil
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(&stdout)
	root.SetIn(stdin)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), err
}

// -F - and -f - read the command's stdin, and every body or entries file is
// read only up to its limit.
func TestJiraFileInputsReadStdinWithLimits(t *testing.T) {
	setupJiraCLIEnv(t)
	fake := jiratest.New()
	fake.JSON("POST /rest/api/3/issue/CSM-1/comment", http.StatusCreated, `{"id":"5"}`)
	if _, err := runAtlasStdin(t, fake.Transport(), strings.NewReader("hello from stdin"), "jira", "comment", "CSM-1", "-F", "-"); err != nil {
		t.Fatal(err)
	}
	if posts := fake.Find("POST /rest/api/3/issue/CSM-1/comment"); len(posts) != 1 || !strings.Contains(posts[0].Body, "hello from stdin") {
		t.Errorf("comment posts = %v", posts)
	}

	fake = jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me"}`)
	fake.IssueWorklogs("10001", "CSM-1", "acc-me")
	entries := `[{"issue":"CSM-1","started":"2026-11-11T09:00:00-05:00","duration":"1h"}]`
	if got, err := runAtlasStdin(t, fake.Transport(), strings.NewReader(entries), "jira", "worklog", "add", "-f", "-"); err != nil || !strings.HasPrefix(got, "0 ok CSM-1") {
		t.Errorf("worklog add from stdin = %q, %v", got, err)
	}

	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 32<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		stdin io.Reader
		args  []string
		want  string
	}{
		{"body stdin", strings.NewReader(strings.Repeat("x", jiraops.MaxBodyBytes+1)), []string{"jira", "comment", "CSM-1", "-F", "-"}, "read body: stdin is over the 131072-byte limit"},
		{"body file", strings.NewReader(""), []string{"jira", "comment", "CSM-1", "-F", big}, "is over the 131072-byte limit"},
		{"entries stdin", strings.NewReader("[" + strings.Repeat(" ", maxEntriesFileBytes) + "]"), []string{"jira", "worklog", "add", "-f", "-"}, "read entries: stdin is over the 1048576-byte limit"},
		{"entries file", strings.NewReader(""), []string{"jira", "worklog", "add", "-f", big}, "is over the 1048576-byte limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := jiratest.New()
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, err := runAtlasStdin(t, fake.Transport(), tc.stdin, tc.args...)
			runtime.ReadMemStats(&after)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "read ") {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
				t.Errorf("allocated %d bytes reading an over-limit input", grew)
			}
			if len(fake.Requests()) != 0 {
				t.Errorf("requests = %v", fake.Routes())
			}
		})
	}
}
