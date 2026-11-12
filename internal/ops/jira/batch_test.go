package jira

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

func worklogFake() *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me","active":true}`)
	logs := fake.IssueWorklogs("30756", "CSM-756", "acc-me")
	logs.Add("acc-me", "2026-11-11T09:30:00.000-0500", 5400)
	logs.Add("acc-other", "2026-11-11T11:00:00.000-0500", 1800)
	fake.IssueWorklogs("30001", "CSM-1", "acc-me")
	for _, ref := range []string{"CSM-1", "30001"} {
		fake.JSON("POST /rest/api/3/issue/"+ref+"/worklog", http.StatusForbidden, `{"errorMessages":["You do not have permission to log work."]}`)
	}
	return fake
}

// worklogRequests returns the worklog requests with method, under an
// issue's key or ID.
func worklogRequests(fake *jiratest.Fake, method string) []jiratest.Request {
	var out []jiratest.Request
	for _, r := range fake.Requests() {
		if r.Method == method && strings.HasSuffix(r.Path, "/worklog") {
			out = append(out, r)
		}
	}
	return out
}

func TestWorklogAddSkipsWhatYouLogged(t *testing.T) {
	fake := worklogFake()
	res, err := invoke(t, fake, WorklogAdd, `{"entries":[
		{"issue":"CSM-756","started":"2026-11-11T14:30:00Z","duration":"1h 30m"},
		{"issue":"CSM-756","started":"2026-11-11T11:00:00-05:00","duration":"30m","comment":"pairing"},
		{"issue":"CSM-756","started":"2026-11-11T11:00:00-05:00","duration":"PT30M"},
		{"issue":"CSM-756","started":"2026-11-11T13:00:00-05:00","duration":"1.5h"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"results":[` +
		`{"index":0,"status":"skipped","issue":"CSM-756","started":"2026-11-11T14:30:00.000+0000","duration":"1h 30m","worklog_id":"900"},` +
		`{"index":1,"status":"ok","issue":"CSM-756","started":"2026-11-11T11:00:00.000-0500","duration":"30m","worklog_id":"902"},` +
		`{"index":2,"status":"skipped","issue":"CSM-756","started":"2026-11-11T11:00:00.000-0500","duration":"30m","worklog_id":"902"},` +
		`{"index":3,"status":"ok","issue":"CSM-756","started":"2026-11-11T13:00:00.000-0500","duration":"1h 30m","worklog_id":"903"}]}`
	if res.Text != want {
		t.Errorf("results =\n%s\nwant\n%s", res.Text, want)
	}
	posts := worklogRequests(fake, "POST")
	if len(posts) != 2 {
		t.Fatalf("posted %d worklogs", len(posts))
	}
	first := body(t, posts[0])
	if first["started"] != "2026-11-11T11:00:00.000-0500" || first["timeSpentSeconds"] != float64(1800) || !strings.Contains(jsonOf(t, first["comment"]), "pairing") {
		t.Errorf("worklog body = %s", jsonOf(t, first))
	}
	for _, get := range worklogRequests(fake, "GET") {
		q, _ := url.ParseQuery(get.Query)
		if q.Get("startedAfter") == "" || q.Get("startedBefore") == "" {
			t.Errorf("existing worklogs not narrowed to the entry's start: %v", q)
		}
	}
}

func TestWorklogAddPartial(t *testing.T) {
	fake := worklogFake()
	res, err := invoke(t, fake, WorklogAdd, `{"entries":[
		{"issue":"CSM-1","started":"2026-11-11T09:00:00-05:00","duration":"1h"},
		{"issue":"CSM-756","started":"2026-11-11T15:00:00-05:00","duration":"45m"}]}`)
	wantKind(t, err, ops.Partial, "1 of 2 worklog entries failed")
	wantContains(t, res.Text, `"index":0,"status":"failed"`, "You do not have permission to log work.", `"index":1,"status":"ok"`)

	fake = worklogFake()
	res, err = invoke(t, fake, WorklogAdd, `{"entries":[{"issue":"CSM-1","started":"2026-11-11T09:00:00-05:00","duration":"1h"}]}`)
	wantKind(t, err, ops.Partial, "1 of 1 worklog entries failed")
	wantContains(t, res.Text, `"status":"failed"`)
}

func TestWorklogAddFailsEntriesItCannotCheck(t *testing.T) {
	fake := worklogFake()
	for _, ref := range []string{"CSM-756", "30756"} {
		fake.JSON("GET /rest/api/3/issue/"+ref+"/worklog", http.StatusInternalServerError, `{"errorMessages":["boom"]}`)
	}
	res, err := invoke(t, fake, WorklogAdd, `{"entries":[{"issue":"CSM-756","started":"2026-11-11T09:00:00-05:00","duration":"1h"}]}`)
	wantKind(t, err, ops.Partial)
	wantContains(t, res.Text, "could not check for an existing worklog")
	if len(worklogRequests(fake, "POST")) != 0 {
		t.Error("logged work it could not check for duplicates")
	}
}

func TestWorklogAddValidatesBeforeWriting(t *testing.T) {
	tests := []struct{ entries, want string }{
		{`[]`, "1 to 200"},
		{`[{"issue":"CSM-756","started":"2026-11-11 09:00","duration":"1h"}]`, "entries[0]: started"},
		{`[{"issue":"CSM-756","started":"2026-11-11T09:00:00","duration":"1h"}]`, "with an offset"},
		{`[{"issue":"CSM-756","started":"2026-11-11T09:00:00Z","duration":"1h"},{"issue":"CSM-756","started":"2026-11-11T09:00:00Z","duration":"1d"}]`, "entries[1]: duration \"1d\": days and weeks"},
		{`[{"issue":"CSM-756","started":"2026-11-11T09:00:00Z","duration":"30s"}]`, "at least one minute"},
		{`[{"issue":"bad","started":"2026-11-11T09:00:00Z","duration":"1h"}]`, "invalid issue"},
		{`[` + strings.TrimSuffix(strings.Repeat(`{"issue":"CSM-756","started":"2026-11-11T09:00:00Z","duration":"1h"},`, MaxWorklogEntries+1), ",") + `]`, "1 to 200"},
	}
	for _, tc := range tests {
		fake := worklogFake()
		_, err := invoke(t, fake, WorklogAdd, `{"entries":`+tc.entries+`}`)
		wantKind(t, err, ops.Invalid, tc.want)
		noWrites(t, fake)
	}
}

func TestParseDuration(t *testing.T) {
	tests := map[string]int{
		"1h 30m": 5400, "1h30m": 5400, "90m": 5400, "1.5h": 5400, "PT1H30M": 5400, "pt45m": 2700,
		"2 hours": 7200, "45 min": 2700, "1h 0m 60s": 3660,
	}
	for in, want := range tests {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "1x", "2d", "1w 2h", "PT", "h"} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("parseDuration(%q) = nil error", bad)
		}
	}
}

func TestWorklogList(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-756/worklog", http.StatusOK, `{"startAt":0,"total":2,"worklogs":[
		{"id":"900","author":{"displayName":"Tony"},"started":"2026-11-11T09:30:00.000-0500","timeSpent":"1h 30m","timeSpentSeconds":5400,
		 "comment":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"triage"}]}]}},
		{"id":"901","author":{"displayName":"Mia"},"started":"2026-11-11T11:00:00.000-0500","timeSpentSeconds":1800}]}`)
	res := mustInvoke(t, fake, WorklogList, `{"issue":"CSM-756"}`)
	want := "Worklogs on CSM-756:\n\n| Started | Author | Time | ID | Comment |\n| --- | --- | --- | --- | --- |\n" +
		"| 2026-11-11 09:30 -0500 | Tony | 1h 30m | 900 | triage |\n| 2026-11-11 11:00 -0500 | Mia | 30m | 901 | — |\n\n2 worklogs, 2h total.\n"
	if res.Text != want {
		t.Errorf("list =\n%s\nwant\n%s", res.Text, want)
	}
}

func TestAttach(t *testing.T) {
	fake := jiratest.New()
	var parts map[string]string
	fake.Handle("POST /rest/api/3/issue/CSM-756/attachments", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			t.Errorf("X-Atlassian-Token = %q", r.Header.Get("X-Atlassian-Token"))
		}
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		parts = map[string]string{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			parts[part.FileName()] = string(data)
		}
		jiratest.Write(w, http.StatusOK, `[{"id":"12001","filename":"notes.txt","size":11},{"id":"12002","filename":"b.log","size":3}]`)
	})
	args := fmt.Sprintf(`{"issue":"CSM-756","files":[{"filename":"notes.txt","content":"%s"},{"filename":"b.log","content":"%s"}]}`,
		base64.StdEncoding.EncodeToString([]byte("hello atlas")), base64.StdEncoding.EncodeToString([]byte("abc")))
	res := mustInvoke(t, fake, Attach, args)
	if parts["notes.txt"] != "hello atlas" || parts["b.log"] != "abc" {
		t.Errorf("uploaded parts = %v", parts)
	}
	wantContains(t, res.Text, `{"id":"12001","filename":"notes.txt","size":11,"url":"https://acme.atlassian.net/rest/api/3/attachment/content/12001"}`)

	for _, tc := range []struct{ files, want string }{
		{`[]`, "1 to 20 files"},
		{`[{"filename":"../etc/passwd","content":"YQ=="}]`, "bare file name"},
		{`[{"filename":"a.txt","content":"not base64!"}]`, "not base64"},
		{`[{"filename":"big.bin","content":"` + base64.StdEncoding.EncodeToString(make([]byte, MaxAttachBytes+1)) + `"}]`, "over the 10485760-byte limit"},
	} {
		fake := jiratest.New()
		_, err := invoke(t, fake, Attach, `{"issue":"CSM-756","files":`+tc.files+`}`)
		wantKind(t, err, ops.Invalid, tc.want)
		noWrites(t, fake)
	}
}

// pages answers every page request with a full page from a set Jira says
// is enormous, counting what it served.
func pages(listKey string, served *int, item func(i int) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.Atoi(q.Get("startAt"))
		size, _ := strconv.Atoi(q.Get("maxResults"))
		items := make([]string, size)
		for i := range items {
			items[i] = item(start + i)
		}
		*served += size
		jiratest.Write(w, http.StatusOK, fmt.Sprintf(`{"startAt":%d,"maxResults":%d,"total":5000000,"isLast":false,"%s":[%s]}`,
			start, size, listKey, strings.Join(items, ",")))
	}
}

func TestLargeSetsStopAtTheirMaximum(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		fake := jiratest.New()
		served, page := 0, 0
		fake.Handle("POST /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
			req := map[string]any{}
			_ = jsonDecode(r, &req)
			size := int(req["maxResults"].(float64))
			if size > searchPageSize {
				t.Errorf("page size %d", size)
			}
			items := make([]string, size)
			for i := range items {
				items[i] = fmt.Sprintf(`{"key":"CSM-%d","fields":{"summary":"s"}}`, served+i)
			}
			served += size
			page++
			jiratest.Write(w, http.StatusOK, fmt.Sprintf(`{"nextPageToken":"t%d","issues":[%s]}`, page, strings.Join(items, ",")))
		})
		res := mustInvoke(t, fake, Search, fmt.Sprintf(`{"jql":"order by created","limit":%d}`, MaxSearchResults))
		if served != MaxSearchResults || len(res.Value.(*SearchResult).Issues) != MaxSearchResults {
			t.Errorf("served %d issues for a limit of %d", served, MaxSearchResults)
		}
		wantContains(t, res.Text, `page_token "t10"`)
	})
	t.Run("view comments and history", func(t *testing.T) {
		fake := jiratest.New()
		fake.JSON("GET /rest/api/3/issue/CSM-1", http.StatusOK, `{"key":"CSM-1","fields":{"summary":"big"}}`)
		comments, history := 0, 0
		fake.Handle("GET /rest/api/3/issue/CSM-1/comment", pages("comments", &comments, func(i int) string {
			return fmt.Sprintf(`{"id":"%d","created":"2026-11-11T09:00:00.000-0500"}`, i)
		}))
		fake.Handle("GET /rest/api/3/issue/CSM-1/changelog", pages("values", &history, func(i int) string {
			return fmt.Sprintf(`{"id":"%d","created":"2026-11-11T09:00:00.000-0500","items":[]}`, i)
		}))
		res := mustInvoke(t, fake, View, fmt.Sprintf(`{"issue":"CSM-1","comments":%d,"history":true}`, MaxViewComments))
		if comments != MaxViewComments {
			t.Errorf("served %d comments for a limit of %d", comments, MaxViewComments)
		}
		if history > MaxHistory+defaultPageSizeForTest {
			t.Errorf("served %d history entries for a limit of %d", history, MaxHistory)
		}
		wantContains(t, res.Text, "## Comments (newest 100 of 5000000)", "## History (newest 500 of 5000000)")
		_, err := invoke(t, fake, View, `{"issue":"CSM-1","comments":-1}`)
		wantKind(t, err, ops.Invalid, "between 1 and 100")
	})
	t.Run("worklogs", func(t *testing.T) {
		fake := jiratest.New()
		served := 0
		fake.Handle("GET /rest/api/3/issue/CSM-1/worklog", pages("worklogs", &served, func(i int) string {
			return fmt.Sprintf(`{"id":"%d","started":"2026-11-11T09:00:00.000-0500","timeSpentSeconds":60}`, i)
		}))
		res := mustInvoke(t, fake, WorklogList, `{"issue":"CSM-1"}`)
		if served != MaxWorklogs {
			t.Errorf("served %d worklogs for a limit of %d", served, MaxWorklogs)
		}
		wantContains(t, res.Text, "First 1000 of 5000000 worklogs")
	})
	t.Run("projects", func(t *testing.T) {
		fake := jiratest.New()
		served := 0
		fake.Handle("GET /rest/api/3/project/search", pages("values", &served, func(i int) string {
			return fmt.Sprintf(`{"id":"%d","key":"P%d","name":"p"}`, i, i)
		}))
		mustInvoke(t, fake, Projects, fmt.Sprintf(`{"limit":%d}`, MaxProjects))
		if served != MaxProjects {
			t.Errorf("served %d projects for a limit of %d", served, MaxProjects)
		}
	})
}

// defaultPageSizeForTest is the first changelog page ChangelogNewest reads
// to learn the total before it jumps to the newest entries.
const defaultPageSizeForTest = 100
