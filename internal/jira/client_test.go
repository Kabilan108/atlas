package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	testEmail = "dev@example.com"
	testToken = "secret-token"
)

// testClient serves handler and fails the test on any request without the
// expected Basic auth.
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testEmail+":"+testToken))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClientWithCredentials(testEmail, testToken, "", WithBaseURL(srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var err error
	if s, ok := v.(string); ok {
		_, err = io.WriteString(w, s)
	} else {
		err = json.NewEncoder(w).Encode(v)
	}
	if err != nil {
		t.Error(err)
	}
}

func expect(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method || r.URL.Path != path {
		t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, method, path)
	}
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func jsonEqual(t *testing.T, got any, want string) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(gotJSON, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON = %s\nwant %s", gotJSON, want)
	}
}

var ctx = context.Background()

func TestNewClientWithCredentials(t *testing.T) {
	c, err := NewClientWithCredentials("a@b.c", "tok", "2a0c794c-e073")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.BaseURL(), "https://api.atlassian.com/ex/jira/2a0c794c-e073"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
	for _, tc := range []struct{ email, token, cloud string }{
		{"", "tok", "cloud"},
		{"a@b.c", "", "cloud"},
		{"a@b.c", "tok", ""},
	} {
		if _, err := NewClientWithCredentials(tc.email, tc.token, tc.cloud); err == nil {
			t.Errorf("NewClientWithCredentials(%q, %q, %q) = nil error", tc.email, tc.token, tc.cloud)
		}
	}
}

func TestOptionsUserAgentAndHTTPClient(t *testing.T) {
	var used bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "atlas-test/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		writeJSON(t, w, 200, `{"accountId":"abc"}`)
	}))
	defer srv.Close()
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return http.DefaultTransport.RoundTrip(r)
	})}
	c, err := NewClientWithCredentials(testEmail, testToken, "cloud", WithBaseURL(srv.URL), WithHTTPClient(hc), WithUserAgent("atlas-test/1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Myself(ctx); err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Error("WithHTTPClient client was not used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAPIError(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		header   map[string]string
		wantMsgs []string
		wantErrs map[string]string
		wantBody string
		is       error
		contains string
	}{
		{
			name:     "platform error collection",
			status:   400,
			body:     `{"errorMessages":["Field 'priority' is required"],"errors":{"customfield_10153":"Option id 'x' is not valid"}}`,
			wantMsgs: []string{"Field 'priority' is required"},
			wantErrs: map[string]string{"customfield_10153": "Option id 'x' is not valid"},
			contains: "customfield_10153: Option id 'x' is not valid",
		},
		{
			name:     "service management",
			status:   404,
			body:     `{"errorMessage":"The request type you are trying to view does not exist.","i18nErrorMessage":{"i18nKey":"sd.request.type.error.notfound","parameters":[]}}`,
			wantMsgs: []string{"The request type you are trying to view does not exist."},
			is:       ErrNotFound,
		},
		{
			name:     "plain body",
			status:   502,
			body:     "<html>Bad gateway</html>",
			wantBody: "<html>Bad gateway</html>",
			is:       ErrServerError,
		},
		{
			name:     "rate limited",
			status:   429,
			body:     ``,
			header:   map[string]string{"Retry-After": "7"},
			is:       ErrRateLimited,
			contains: "Too Many Requests",
		},
		{name: "unauthorized", status: 401, body: `{"message":"Client must be authenticated"}`, wantMsgs: []string{"Client must be authenticated"}, is: ErrUnauthorized},
		{name: "forbidden", status: 403, body: `{"errorMessages":["no"]}`, wantMsgs: []string{"no"}, is: ErrForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := c.GetIssue(ctx, "ED-1", GetIssueOptions{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.HTTPStatus() != tc.status {
				t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus(), tc.status)
			}
			if !reflect.DeepEqual(apiErr.ErrorMessages, tc.wantMsgs) {
				t.Errorf("ErrorMessages = %#v, want %#v", apiErr.ErrorMessages, tc.wantMsgs)
			}
			if !reflect.DeepEqual(apiErr.Errors, tc.wantErrs) {
				t.Errorf("Errors = %#v, want %#v", apiErr.Errors, tc.wantErrs)
			}
			if apiErr.Body != tc.wantBody {
				t.Errorf("Body = %q, want %q", apiErr.Body, tc.wantBody)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("errors.Is(%v) = false", tc.is)
			}
			if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("Error() = %q, want it to contain %q", err.Error(), tc.contains)
			}
			if !strings.Contains(err.Error(), "GET /rest/api/3/issue/ED-1") {
				t.Errorf("Error() = %q, want method and path", err.Error())
			}
			if tc.header["Retry-After"] == "7" && apiErr.RetryAfter != 7*time.Second {
				t.Errorf("RetryAfter = %v", apiErr.RetryAfter)
			}
		})
	}
}

func TestGetIssue(t *testing.T) {
	fixture, err := os.ReadFile("testdata/issue.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		expect(t, r, http.MethodGet, "/rest/api/3/issue/ED-1")
		if got := r.URL.Query().Get("fields"); got != "summary,status,customfield_10153" {
			t.Errorf("fields = %q", got)
		}
		if got := r.URL.Query().Get("expand"); got != "names,renderedFields,changelog" {
			t.Errorf("expand = %q", got)
		}
		writeJSON(t, w, 200, string(fixture))
	})
	issue, err := c.GetIssue(ctx, "ED-1", GetIssueOptions{
		Fields: []string{"summary", "status", "customfield_10153"},
		Expand: []string{"names", "renderedFields", "changelog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := issue.Fields
	if issue.Key != "ED-1" || f.Summary != "Login fails on staging" || f.Status.Name != "In Progress" {
		t.Errorf("issue = %s %q %v", issue.Key, f.Summary, f.Status)
	}
	if f.Assignee.AccountID != "5b10a2844c20165700ede21g" || f.Priority.Name != "Medium" || f.Parent.Key != "ED-0" || f.Parent.Fields.Summary != "Auth epic" {
		t.Errorf("assignee/priority/parent = %v %v %v", f.Assignee, f.Priority, f.Parent)
	}
	if len(f.IssueLinks) != 2 || f.IssueLinks[0].OutwardIssue.Key != "PR-2" || f.IssueLinks[1].InwardIssue.Key != "PR-3" {
		t.Errorf("links = %+v", f.IssueLinks)
	}
	if len(f.Attachments) != 1 || f.Attachments[0].Filename != "picture.jpg" {
		t.Errorf("attachments = %+v", f.Attachments)
	}
	if f.Comment == nil || len(f.Comment.Comments) != 1 || f.Comment.Comments[0].Visibility.Value != "Administrators" {
		t.Errorf("comment = %+v", f.Comment)
	}
	if !reflect.DeepEqual(f.Labels, []string{"auth", "staging"}) {
		t.Errorf("labels = %v", f.Labels)
	}
	id, raw, ok := issue.FieldByName("product")
	if !ok || id != "customfield_10153" || !strings.Contains(string(raw), `"Atlas"`) {
		t.Errorf("FieldByName(product) = %q %s %v", id, raw, ok)
	}
	if issue.Schema["customfield_10153"].Items != "option" {
		t.Errorf("schema = %+v", issue.Schema)
	}
	if string(issue.RenderedFields["description"]) != `"<p>Main order flow broken</p>"` {
		t.Errorf("renderedFields = %s", issue.RenderedFields["description"])
	}
	if issue.Changelog == nil || issue.Changelog.Histories[0].Items[0].ToString != "In Progress" {
		t.Errorf("changelog = %+v", issue.Changelog)
	}

	again, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	var round Issue
	if err := json.Unmarshal(again, &round); err != nil {
		t.Fatal(err)
	}
	if round.Fields.Summary != f.Summary || string(round.RawFields["customfield_10469"]) != `"1.4.2"` {
		t.Errorf("marshal round trip lost fields: %s", again)
	}
}

func TestGetIssueEscapesKey(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/rest/api/3/issue/ED-1%2F..%2Fx" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		writeJSON(t, w, 200, `{"id":"1","key":"ED-1","fields":{}}`)
	})
	if _, err := c.GetIssue(ctx, "ED-1/../x", GetIssueOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestListChangelogPaginates(t *testing.T) {
	var starts []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		expect(t, r, http.MethodGet, "/rest/api/3/issue/ED-1/changelog")
		start := r.URL.Query().Get("startAt")
		starts = append(starts, start)
		if start == "0" {
			writeJSON(t, w, 200, `{"startAt":0,"maxResults":2,"total":3,"isLast":false,"values":[{"id":"1","items":[]},{"id":"2","items":[]}]}`)
			return
		}
		writeJSON(t, w, 200, `{"startAt":2,"maxResults":2,"total":3,"isLast":true,"values":[{"id":"3","items":[]}]}`)
	})
	hist, err := c.ListChangelog(ctx, "ED-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 || hist[2].ID != "3" || !reflect.DeepEqual(starts, []string{"0", "2"}) {
		t.Errorf("histories = %d, starts = %v", len(hist), starts)
	}
}

func TestSearchFollowsNextPageToken(t *testing.T) {
	const jql = `project = ED AND status != Done ORDER BY updated DESC, key ASC`
	var tokens []any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		expect(t, r, http.MethodPost, "/rest/api/3/search/jql")
		body := readBody(t, r)
		if body["jql"] != jql {
			t.Errorf("jql = %q", body["jql"])
		}
		if !reflect.DeepEqual(body["fields"], []any{"summary", "status"}) {
			t.Errorf("fields = %v", body["fields"])
		}
		if body["maxResults"] != float64(2) {
			t.Errorf("maxResults = %v", body["maxResults"])
		}
		tokens = append(tokens, body["nextPageToken"])
		switch body["nextPageToken"] {
		case nil:
			writeJSON(t, w, 200, `{"issues":[{"id":"1","key":"ED-1","fields":{"summary":"a"}},{"id":"2","key":"ED-2","fields":{"summary":"b"}}],"nextPageToken":"tok-2","isLast":false}`)
		case "tok-2":
			writeJSON(t, w, 200, `{"issues":[{"id":"3","key":"ED-3","fields":{"summary":"c"}}],"isLast":true}`)
		default:
			t.Errorf("unexpected token %v", body["nextPageToken"])
		}
	})
	issues, err := c.Search(ctx, SearchRequest{JQL: jql, Fields: []string{"summary", "status"}, MaxResults: 2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 3 || issues[2].Key != "ED-3" || issues[1].Fields.Summary != "b" {
		t.Errorf("issues = %+v", issues)
	}
	if !reflect.DeepEqual(tokens, []any{nil, "tok-2"}) {
		t.Errorf("tokens = %v", tokens)
	}
}

func TestSearchLimitAndPage(t *testing.T) {
	var sizes []any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		sizes = append(sizes, body["maxResults"])
		writeJSON(t, w, 200, `{"issues":[{"id":"1","key":"ED-1","fields":{}},{"id":"2","key":"ED-2","fields":{}}],"nextPageToken":"more"}`)
	})
	issues, err := c.Search(ctx, SearchRequest{JQL: "project = ED", MaxResults: 2}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 3 || !reflect.DeepEqual(sizes, []any{float64(2), float64(1)}) {
		t.Errorf("issues = %d, sizes = %v", len(issues), sizes)
	}

	page, err := c.SearchPage(ctx, SearchRequest{JQL: "project = ED", NextPageToken: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if page.IsLast || page.NextPageToken != "more" || len(page.Issues) != 2 {
		t.Errorf("page = %+v", page)
	}
}

func TestSearchStopsOnRepeatedToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, `{"issues":[{"id":"1","key":"ED-1","fields":{}}],"nextPageToken":"same","isLast":false}`)
	})
	if _, err := c.Search(ctx, SearchRequest{JQL: "x"}, 0); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("err = %v, want repeated-token error", err)
	}
}

func TestSearchMissingTokenIsLast(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(t, w, 200, `{"issues":[{"id":"1","key":"ED-1","fields":{}}]}`)
	})
	if _, err := c.Search(ctx, SearchRequest{JQL: "x"}, 0); err != nil || calls != 1 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}

func TestCreateAndEditIssue(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			expect(t, r, http.MethodPost, "/rest/api/3/issue")
			jsonEqual(t, readBody(t, r), `{"fields":{"project":{"key":"ED"},"issuetype":{"id":"10000"},"summary":"New bug","customfield_10153":[{"value":"Atlas"}]}}`)
			writeJSON(t, w, 201, `{"id":"10000","key":"ED-24","self":"https://x/rest/api/3/issue/10000"}`)
		case http.MethodPut:
			expect(t, r, http.MethodPut, "/rest/api/3/issue/ED-24")
			if got := r.URL.Query().Get("notifyUsers"); got != "false" {
				t.Errorf("notifyUsers = %q", got)
			}
			jsonEqual(t, readBody(t, r), `{"fields":{"summary":"Renamed"},"update":{"labels":[{"add":"triaged"}]}}`)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	created, err := c.CreateIssue(ctx, IssueInput{Fields: map[string]any{
		"project":           map[string]any{"key": "ED"},
		"issuetype":         map[string]any{"id": "10000"},
		"summary":           "New bug",
		"customfield_10153": []any{map[string]any{"value": "Atlas"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Key != "ED-24" {
		t.Errorf("created = %+v", created)
	}
	notify := false
	err = c.EditIssue(ctx, "ED-24", IssueInput{
		Fields: map[string]any{"summary": "Renamed"},
		Update: map[string][]map[string]any{"labels": {{"add": "triaged"}}},
	}, EditIssueOptions{NotifyUsers: &notify})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateMetaPaginates(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta/ED/issuetypes":
			if q.Get("startAt") == "0" {
				writeJSON(t, w, 200, `{"startAt":0,"maxResults":1,"total":2,"issueTypes":[{"id":"10000","name":"Bug","subtask":false}]}`)
				return
			}
			if q.Get("startAt") != "1" {
				t.Errorf("startAt = %q", q.Get("startAt"))
			}
			writeJSON(t, w, 200, `{"startAt":1,"maxResults":1,"total":2,"issueTypes":[{"id":"10001","name":"Task","subtask":false}]}`)
		case "/rest/api/3/issue/createmeta/ED/issuetypes/10000":
			if q.Get("startAt") == "0" {
				writeJSON(t, w, 200, `{"startAt":0,"maxResults":2,"total":3,"values":[
					{"fieldId":"summary","key":"summary","name":"Summary","required":true,"schema":{"type":"string","system":"summary"},"operations":["set"]},
					{"fieldId":"customfield_10153","key":"customfield_10153","name":"Product","required":false,"schema":{"type":"array","items":"option","custom":"com.atlassian.jira.plugin.system.customfieldtypes:multiselect","customId":10153},"allowedValues":[{"self":"x","value":"Atlas","id":"10200"},{"self":"x","value":"Beacon","id":"10201"}]}]}`)
				return
			}
			writeJSON(t, w, 200, `{"startAt":2,"maxResults":2,"total":3,"values":[{"fieldId":"customfield_10469","name":"Software Version","required":false,"schema":{"type":"string","custom":"com.atlassian.jira.plugin.system.customfieldtypes:textfield","customId":10469}}]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	types, err := c.CreateMetaIssueTypes(ctx, "ED")
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 || types[1].Name != "Task" {
		t.Errorf("types = %+v", types)
	}
	fields, err := c.CreateMetaFields(ctx, "ED", "10000")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields[1].Schema.Items != "option" || fields[1].AllowedValues[1].Value != "Beacon" || fields[2].ID() != "customfield_10469" {
		t.Errorf("fields = %+v", fields)
	}
}

func TestEditMeta(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		expect(t, r, http.MethodGet, "/rest/api/3/issue/ED-1/editmeta")
		writeJSON(t, w, 200, `{"fields":{
			"summary":{"required":true,"schema":{"type":"string","system":"summary"},"name":"Summary","key":"summary","operations":["set"]},
			"labels":{"required":false,"schema":{"type":"array","items":"string","system":"labels"},"name":"Labels","key":"labels","autoCompleteUrl":"https://x/rest/api/1.0/labels/suggest?query=","operations":["add","set","remove"]}}}`)
	})
	fields, err := c.EditMeta(ctx, "ED-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[0].Name != "Labels" || fields[0].FieldID != "labels" || fields[1].ID() != "summary" {
		t.Errorf("fields = %+v", fields)
	}
}

func TestTransitions(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			expect(t, r, http.MethodGet, "/rest/api/3/issue/ED-1/transitions")
			if got := r.URL.Query().Get("expand"); got != "transitions.fields" {
				t.Errorf("expand = %q", got)
			}
			writeJSON(t, w, 200, `{"expand":"transitions","transitions":[
				{"id":"2","name":"Close Issue","to":{"id":"10000","name":"Closed","statusCategory":{"id":3,"key":"done","name":"Done"}},"hasScreen":false,"isGlobal":false,"isInitial":false,"isAvailable":true,"isConditional":false,
				 "fields":{"resolution":{"required":true,"schema":{"type":"resolution","system":"resolution"},"name":"Resolution","key":"resolution","operations":["set"],"allowedValues":[{"id":"10000","name":"Fixed"}]}}},
				{"id":"711","name":"Resolve (2)","to":{"id":"5","name":"Resolved (2)"}}]}`)
		case http.MethodPost:
			expect(t, r, http.MethodPost, "/rest/api/3/issue/ED-1/transitions")
			jsonEqual(t, readBody(t, r), `{"transition":{"id":"2"},"fields":{"resolution":{"name":"Fixed"}}}`)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	ts, err := c.ListTransitions(ctx, "ED-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[0].To.Name != "Closed" || !ts[0].Fields["resolution"].Required || ts[0].Fields["resolution"].AllowedValues[0].Name != "Fixed" {
		t.Errorf("transitions = %+v", ts)
	}
	if err := c.DoTransition(ctx, "ED-1", "2", TransitionInput{Fields: map[string]any{"resolution": map[string]any{"name": "Fixed"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestIssueLinks(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			expect(t, r, http.MethodGet, "/rest/api/3/issueLinkType")
			writeJSON(t, w, 200, `{"issueLinkTypes":[{"id":"1000","name":"Duplicate","inward":"Duplicated by","outward":"Duplicates"},{"id":"1010","name":"2 Relates","inward":"relates to","outward":"relates to"}]}`)
		case http.MethodPost:
			expect(t, r, http.MethodPost, "/rest/api/3/issueLink")
			jsonEqual(t, readBody(t, r), `{"type":{"name":"2 Relates"},"inwardIssue":{"key":"ED-1"},"outwardIssue":{"key":"ED-2"},"comment":{"body":{"type":"doc","version":1,"content":[]}}}`)
			w.WriteHeader(http.StatusCreated)
		}
	})
	types, err := c.ListIssueLinkTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 || types[1].Outward != "relates to" {
		t.Errorf("types = %+v", types)
	}
	err = c.CreateIssueLink(ctx, LinkInput{TypeName: "2 Relates", InwardIssue: "ED-1", OutwardIssue: "ED-2", Comment: map[string]any{"type": "doc", "version": 1, "content": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssignAndUserSearch(t *testing.T) {
	var assignBodies []map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/rest/api/3/issue/ED-1/assignee":
			expect(t, r, http.MethodPut, "/rest/api/3/issue/ED-1/assignee")
			assignBodies = append(assignBodies, readBody(t, r))
			w.WriteHeader(http.StatusNoContent)
		case "/rest/api/3/user/assignable/search":
			if q.Get("issueKey") != "ED-1" || q.Get("query") != "mia@example.com" || q.Get("maxResults") != "5" || q.Has("project") {
				t.Errorf("assignable query = %v", q)
			}
			writeJSON(t, w, 200, `[{"accountId":"5b10a2844c20165700ede21g","accountType":"atlassian","displayName":"Mia Krystof","active":true}]`)
		case "/rest/api/3/user/search":
			if q.Get("query") != "emma" || q.Get("maxResults") != "10" {
				t.Errorf("search query = %v", q)
			}
			writeJSON(t, w, 200, `[{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Emma Richards","active":true}]`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	if err := c.AssignIssue(ctx, "ED-1", "5b10a2844c20165700ede21g"); err != nil {
		t.Fatal(err)
	}
	if err := c.AssignIssue(ctx, "ED-1", ""); err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, assignBodies, `[{"accountId":"5b10a2844c20165700ede21g"},{"accountId":null}]`)

	users, err := c.SearchAssignableUsers(ctx, AssignableSearch{IssueKey: "ED-1", Query: "mia@example.com", MaxResults: 5})
	if err != nil || len(users) != 1 || users[0].DisplayName != "Mia Krystof" {
		t.Errorf("assignable = %+v, %v", users, err)
	}
	users, err = c.SearchUsers(ctx, "emma", 10)
	if err != nil || len(users) != 1 || users[0].AccountID != "5b10ac8d82e05b22cc7d4ef5" {
		t.Errorf("users = %+v, %v", users, err)
	}
}

func TestComments(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			expect(t, r, http.MethodGet, "/rest/api/3/issue/SD-7/comment")
			if q.Get("orderBy") != "-created" || q.Get("expand") != "renderedBody" {
				t.Errorf("query = %v", q)
			}
			if q.Get("startAt") == "0" {
				writeJSON(t, w, 200, `{"startAt":0,"maxResults":2,"total":3,"comments":[
					{"id":"3","body":{"type":"doc","version":1,"content":[]},"created":"2026-11-03T10:00:00.000+0000","jsdPublic":true},
					{"id":"2","body":{"type":"doc","version":1,"content":[]},"created":"2026-11-02T10:00:00.000+0000","jsdPublic":false}]}`)
				return
			}
			writeJSON(t, w, 200, `{"startAt":2,"maxResults":2,"total":3,"comments":[{"id":"1","created":"2026-11-01T10:00:00.000+0000"}]}`)
		case http.MethodPost:
			expect(t, r, http.MethodPost, "/rest/api/3/issue/SD-7/comment")
			jsonEqual(t, readBody(t, r), `{"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]},"visibility":{"type":"role","value":"Administrators"}}`)
			writeJSON(t, w, 201, `{"id":"10010","created":"2026-11-11T10:00:00.000+0000","body":{"type":"doc","version":1,"content":[]}}`)
		case http.MethodDelete:
			expect(t, r, http.MethodDelete, "/rest/api/3/issue/SD-7/comment/10010")
			w.WriteHeader(http.StatusNoContent)
		}
	})
	comments, err := c.ListComments(ctx, "SD-7", CommentListOptions{OrderBy: "-created", Expand: []string{"renderedBody"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 3 {
		t.Fatalf("comments = %d", len(comments))
	}
	if comments[0].JSDPublic == nil || !*comments[0].JSDPublic {
		t.Errorf("comment 3 jsdPublic = %v, want true", comments[0].JSDPublic)
	}
	if comments[1].JSDPublic == nil || *comments[1].JSDPublic {
		t.Errorf("comment 2 jsdPublic = %v, want false", comments[1].JSDPublic)
	}
	if comments[2].JSDPublic != nil {
		t.Errorf("comment 1 jsdPublic = %v, want nil (missing)", *comments[2].JSDPublic)
	}

	limited, err := c.ListComments(ctx, "SD-7", CommentListOptions{OrderBy: "-created", Expand: []string{"renderedBody"}, Limit: 1})
	if err != nil || len(limited) != 1 || limited[0].ID != "3" {
		t.Errorf("limited = %+v, %v", limited, err)
	}

	page, err := c.ListCommentsPage(ctx, "SD-7", 2, 2, CommentListOptions{OrderBy: "-created", Expand: []string{"renderedBody"}})
	if err != nil || page.StartAt != 2 || len(page.Comments) != 1 {
		t.Errorf("page = %+v, %v", page, err)
	}

	body := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]}`)
	added, err := c.AddComment(ctx, "SD-7", body, &Visibility{Type: "role", Value: "Administrators"})
	if err != nil || added.ID != "10010" {
		t.Errorf("added = %+v, %v", added, err)
	}
	if err := c.DeleteComment(ctx, "SD-7", "10010"); err != nil {
		t.Fatal(err)
	}
}

func TestWorklogs(t *testing.T) {
	var starts []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			expect(t, r, http.MethodGet, "/rest/api/3/issue/ED-1/worklog")
			if q.Get("startedAfter") != "1762000000000" || q.Has("startedBefore") {
				t.Errorf("query = %v", q)
			}
			starts = append(starts, q.Get("startAt"))
			if q.Get("startAt") == "0" {
				writeJSON(t, w, 200, `{"startAt":0,"maxResults":1,"total":2,"worklogs":[{"id":"100028","issueId":"10002","started":"2026-11-03T09:00:00.000-0400","timeSpent":"3h 20m","timeSpentSeconds":12000,"author":{"accountId":"a","displayName":"Mia"}}]}`)
				return
			}
			writeJSON(t, w, 200, `{"startAt":1,"maxResults":1,"total":2,"worklogs":[{"id":"100029","started":"2026-11-04T09:00:00.000-0400","timeSpent":"1h","timeSpentSeconds":3600}]}`)
		case http.MethodPost:
			expect(t, r, http.MethodPost, "/rest/api/3/issue/ED-1/worklog")
			if q.Get("adjustEstimate") != "leave" || q.Get("notifyUsers") != "false" {
				t.Errorf("query = %v", q)
			}
			jsonEqual(t, readBody(t, r), `{"started":"2026-11-11T09:30:00.000-0400","timeSpent":"1h 30m","comment":{"type":"doc","version":1,"content":[]}}`)
			writeJSON(t, w, 201, `{"id":"100030","started":"2026-11-11T09:30:00.000-0400","timeSpent":"1h 30m","timeSpentSeconds":5400}`)
		}
	})
	logs, err := c.ListWorklogs(ctx, "ED-1", WorklogListOptions{StartedAfter: 1762000000000})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].TimeSpentSeconds != 12000 || logs[1].ID != "100029" || !reflect.DeepEqual(starts, []string{"0", "1"}) {
		t.Errorf("logs = %+v starts = %v", logs, starts)
	}
	notify := false
	w, err := c.AddWorklog(ctx, "ED-1", WorklogInput{
		Started:   "2026-11-11T09:30:00.000-0400",
		TimeSpent: "1h 30m",
		Comment:   json.RawMessage(`{"type":"doc","version":1,"content":[]}`),
	}, WorklogAddOptions{AdjustEstimate: "leave", NotifyUsers: &notify})
	if err != nil || w.TimeSpentSeconds != 5400 {
		t.Errorf("worklog = %+v, %v", w, err)
	}
}

func TestUploadAttachments(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		expect(t, r, http.MethodPost, "/rest/api/3/issue/ED-1/attachments")
		if got := r.Header.Get("X-Atlassian-Token"); got != "no-check" {
			t.Errorf("X-Atlassian-Token = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		files := r.MultipartForm.File["file"]
		if len(files) != 2 || files[0].Filename != "log.txt" || files[1].Filename != "shot.png" {
			t.Fatalf("files = %+v", files)
		}
		f, _ := files[0].Open()
		data, _ := io.ReadAll(f)
		if string(data) != "line one\n" {
			t.Errorf("content = %q", data)
		}
		writeJSON(t, w, 200, `[{"id":"10001","filename":"log.txt","size":9,"mimeType":"text/plain","content":"https://x/attachment/content/10001"},{"id":"10002","filename":"shot.png","size":3,"mimeType":"image/png"}]`)
	})
	atts, err := c.UploadAttachments(ctx, "ED-1", []AttachmentFile{
		{Name: "log.txt", Content: strings.NewReader("line one\n")},
		{Name: "shot.png", Content: strings.NewReader("png")},
	})
	if err != nil || len(atts) != 2 || atts[0].ID != "10001" {
		t.Errorf("attachments = %+v, %v", atts, err)
	}
	if _, err := c.UploadAttachments(ctx, "ED-1", nil); err == nil {
		t.Error("UploadAttachments(nil) = nil error")
	}
}

func TestMyselfAndServerInfo(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			writeJSON(t, w, 200, `{"accountId":"5b10a2844c20165700ede21g","accountType":"atlassian","emailAddress":"mia@example.com","displayName":"Mia Krystof","active":true,"timeZone":"Australia/Sydney"}`)
		case "/rest/api/3/serverInfo":
			writeJSON(t, w, 200, `{"baseUrl":"https://example.atlassian.net","version":"1001.0.0-SNAPSHOT","deploymentType":"Cloud","serverTitle":"Jira"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	me, err := c.Myself(ctx)
	if err != nil || me.EmailAddress != "mia@example.com" || me.TimeZone != "Australia/Sydney" {
		t.Errorf("me = %+v, %v", me, err)
	}
	info, err := c.ServerInfo(ctx)
	if err != nil || info.BaseURL != "https://example.atlassian.net" {
		t.Errorf("info = %+v, %v", info, err)
	}
}

func TestProjects(t *testing.T) {
	var starts []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/rest/api/3/project/search":
			if q.Get("query") != "ed" || q.Get("orderBy") != "key" || q.Get("maxResults") != "50" {
				t.Errorf("query = %v", q)
			}
			starts = append(starts, q.Get("startAt"))
			if q.Get("startAt") == "0" {
				writeJSON(t, w, 200, `{"startAt":0,"maxResults":2,"total":3,"isLast":false,"values":[{"id":"10000","key":"ED","name":"Edison","projectTypeKey":"software","simplified":false},{"id":"10001","key":"EDU","name":"Education","projectTypeKey":"business"}]}`)
				return
			}
			writeJSON(t, w, 200, `{"startAt":2,"maxResults":2,"total":3,"isLast":true,"values":[{"id":"10002","key":"SD","name":"Support","projectTypeKey":"service_desk"}]}`)
		case "/rest/api/3/project/SD":
			writeJSON(t, w, 200, `{"id":"10002","key":"SD","name":"Support","projectTypeKey":"service_desk","simplified":true,"style":"next-gen","lead":{"accountId":"a","displayName":"Mia"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	projects, err := c.ListProjects(ctx, ProjectListOptions{Query: "ed", OrderBy: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 3 || projects[2].ProjectTypeKey != "service_desk" || !reflect.DeepEqual(starts, []string{"0", "2"}) {
		t.Errorf("projects = %+v starts = %v", projects, starts)
	}
	p, err := c.GetProject(ctx, "SD")
	if err != nil || p.ProjectTypeKey != "service_desk" || !p.Simplified || p.Lead.DisplayName != "Mia" {
		t.Errorf("project = %+v, %v", p, err)
	}
}

func TestServiceDesk(t *testing.T) {
	var deskStarts []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/rest/servicedeskapi/servicedesk":
			deskStarts = append(deskStarts, q.Get("start"))
			if q.Get("limit") != "50" {
				t.Errorf("limit = %q", q.Get("limit"))
			}
			if q.Get("start") == "0" {
				writeJSON(t, w, 200, `{"size":1,"start":0,"limit":1,"isLastPage":false,"values":[{"id":"1","projectId":"10002","projectName":"Support","projectKey":"SD"}]}`)
				return
			}
			writeJSON(t, w, 200, `{"size":1,"start":1,"limit":1,"isLastPage":true,"values":[{"id":"2","projectId":"10003","projectName":"IT","projectKey":"IT"}]}`)
		case r.URL.Path == "/rest/servicedeskapi/request/SD-7/comment" && r.Method == http.MethodPost:
			jsonEqual(t, readBody(t, r), `{"body":"Internal note for agents","public":false}`)
			writeJSON(t, w, 201, `{"id":"1000","body":"Internal note for agents","public":false,"author":{"accountId":"qm:a","displayName":"Mia"},"created":{"iso8601":"2026-11-11T10:00:00-0400","jira":"2026-11-11T10:00:00.000-0400","friendly":"Today 10:00 AM","epochMillis":1762869600000}}`)
		case r.URL.Path == "/rest/servicedeskapi/request/SD-7/comment" && r.Method == http.MethodGet:
			if q.Get("public") != "false" || q.Get("internal") != "true" || q.Get("start") != "0" {
				t.Errorf("query = %v", q)
			}
			writeJSON(t, w, 200, `{"size":1,"start":0,"limit":50,"isLastPage":true,"values":[{"id":"1000","body":"Internal note for agents","public":false}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	desks, err := c.ListServiceDesks(ctx)
	if err != nil || len(desks) != 2 || desks[1].ProjectKey != "IT" || !reflect.DeepEqual(deskStarts, []string{"0", "1"}) {
		t.Errorf("desks = %+v starts = %v err = %v", desks, deskStarts, err)
	}
	comment, err := c.CreateRequestComment(ctx, "SD-7", "Internal note for agents", false)
	if err != nil || comment.Public || comment.Created.EpochMillis != 1762869600000 {
		t.Errorf("comment = %+v, %v", comment, err)
	}
	public, internal := false, true
	comments, err := c.ListRequestComments(ctx, "SD-7", RequestCommentListOptions{Public: &public, Internal: &internal})
	if err != nil || len(comments) != 1 || comments[0].Public {
		t.Errorf("comments = %+v, %v", comments, err)
	}
}

func TestCollectOffsetStopsOnEmptyAndTotal(t *testing.T) {
	calls := 0
	got, err := collectOffset(0, 10, func(startAt, maxResults int) ([]int, pageInfo, error) {
		calls++
		if startAt == 0 {
			return []int{1, 2}, pageInfo{StartAt: 0, MaxResults: 2, Total: 4}, nil
		}
		return nil, pageInfo{StartAt: startAt, Total: 4}, nil
	})
	if err != nil || !reflect.DeepEqual(got, []int{1, 2}) || calls != 2 {
		t.Errorf("got %v calls %d err %v", got, calls, err)
	}
}
