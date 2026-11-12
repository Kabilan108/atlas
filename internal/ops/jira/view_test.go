package jira

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

const issueJSON = `{
  "id": "29794", "key": "CSM-698",
  "fields": {
    "summary": "Aggregator stop blocks on disconnected drivers",
    "status": {"id": "10008", "name": "In Progress"},
    "issuetype": {"id": "10010", "name": "Bug"},
    "priority": {"id": "2", "name": "High"},
    "project": {"id": "10036", "key": "CSM", "name": "Customer Service", "projectTypeKey": "service_desk"},
    "assignee": {"accountId": "acc-tony", "displayName": "Tony Okeke"},
    "reporter": {"accountId": "acc-mia", "displayName": "Mia Krystof"},
    "parent": {"id": "1", "key": "CSM-600", "fields": {"summary": "Aggregator epic"}},
    "labels": ["aggregator", "field"],
    "created": "2026-10-13T17:04:37.055-0400",
    "updated": "2026-10-19T10:46:59.327-0400",
    "timetracking": {"timeSpent": "30m", "timeSpentSeconds": 1800},
    "issuelinks": [
      {"id": "500", "type": {"id": "10000", "name": "Blocks", "inward": "is blocked by", "outward": "blocks"},
       "outwardIssue": {"key": "CSM-700", "fields": {"summary": "Gateway restart", "status": {"name": "Open"}}}},
      {"id": "501", "type": {"id": "10003", "name": "2 Relates", "inward": "relates to", "outward": "relates to"},
       "inwardIssue": {"key": "MCP-7401", "fields": {"summary": "Driver timeouts", "status": {"name": "Done"}}}}
    ],
    "attachment": [
      {"id": "10001", "filename": "stop-trace.png", "size": 2048, "mimeType": "image/png", "created": "2026-10-13T17:05:00.000-0400", "author": {"displayName": "Mia Krystof"}},
      {"id": "10002", "filename": "aggregator.log", "size": 512, "mimeType": "text/plain", "created": "2026-10-13T17:06:00.000-0400"}
    ],
    "description": {"type": "doc", "version": 1, "content": [
      {"type": "paragraph", "content": [{"type": "text", "text": "Stopping blocks for minutes."}]},
      {"type": "mediaSingle", "content": [{"type": "media", "attrs": {"id": "m1", "type": "file", "collection": ""}}]},
      {"type": "mediaSingle", "content": [{"type": "media", "attrs": {"id": "m-unknown", "type": "file", "collection": ""}}]},
      {"type": "table", "content": [{"type": "tableRow", "content": [
        {"type": "tableCell", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "no header"}]}]}]}]}
    ]},
    "customfield_10153": [{"id": "10237", "value": "MCP Data Aggregator"}, {"id": "10238", "value": "Connect"}],
    "customfield_10469": "2.4.1-dev",
    "customfield_10019": "1|i01h8k:",
    "customfield_10050": {"id": "3", "name": "Time to resolution", "completedCycles": []},
    "customfield_10000": "{}",
    "customfield_10200": {"type": "doc", "version": 1, "content": [
      {"type": "paragraph", "content": [{"type": "text", "text": "1. Stop the gateway"}]},
      {"type": "paragraph", "content": [{"type": "text", "text": "2. Watch it hang"}]}]},
    "customfield_10300": null
  },
  "names": {"customfield_10153": "Product", "customfield_10469": "Software Version", "customfield_10019": "Rank",
            "customfield_10050": "Time to resolution", "customfield_10000": "Development", "customfield_10200": "Steps to Reproduce",
            "customfield_10300": "Empty"},
  "schema": {"customfield_10153": {"type": "array", "items": "option", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:multiselect"},
             "customfield_10469": {"type": "string", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:textfield"},
             "customfield_10019": {"type": "any", "custom": "com.pyxis.greenhopper.jira:gh-lexo-rank"},
             "customfield_10050": {"type": "sd-servicelevelagreement"},
             "customfield_10000": {"type": "any"},
             "customfield_10200": {"type": "string", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:textarea"}},
  "renderedFields": {
    "description": "<p>Stopping blocks.</p><span class=\"image-wrap\"><img src=\"https://acme.atlassian.net/rest/api/3/attachment/thumbnail/10001\" data-attachment-name=\"stop-trace.png\" data-media-services-id=\"m1\" /></span>"
  }
}`

const commentsJSON = `{"startAt": 0, "maxResults": 2, "total": 12, "comments": [
  {"id": "15191", "author": {"displayName": "Tony Okeke"}, "created": "2026-10-15T09:00:00.000-0400", "jsdPublic": false,
   "body": {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Internal: driver issue."}]}]}},
  {"id": "15190", "author": {"displayName": "Mia Krystof"}, "created": "2026-10-14T08:00:00.000-0400", "jsdPublic": true,
   "body": {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Customer reply "},
     {"type": "text", "text": "underlined", "marks": [{"type": "underline"}]}]}]}}
]}`

func viewFake() *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-698", http.StatusOK, issueJSON)
	fake.JSON("GET /rest/api/3/issue/CSM-698/comment", http.StatusOK, commentsJSON)
	fake.JSON("GET /rest/api/3/issue/CSM-698/changelog", http.StatusOK, `{"startAt":0,"maxResults":100,"total":1,"isLast":true,"values":[
		{"id":"1","author":{"displayName":"Tony Okeke"},"created":"2026-10-14T10:00:00.000-0400","items":[
			{"field":"status","fromString":"Open","toString":"In Progress"},{"field":"assignee","fromString":"","toString":"Tony Okeke"}]}]}`)
	return fake
}

func TestViewMarkdown(t *testing.T) {
	fake := viewFake()
	res := mustInvoke(t, fake, View, `{"issue":"https://acme.atlassian.net/browse/csm-698","comments":2,"history":true}`)
	wantContains(t, res.Text,
		"# CSM-698: Aggregator stop blocks on disconnected drivers\n\nhttps://acme.atlassian.net/browse/CSM-698\n",
		"> Shown approximately: complex table x1, media x2, underline x1. Open the issue in Jira for those.",
		"- **Status:** In Progress\n- **Type:** Bug\n- **Priority:** High\n- **Project:** Customer Service (CSM)\n- **Assignee:** Tony Okeke\n- **Reporter:** Mia Krystof\n- **Parent:** CSM-600 Aggregator epic\n- **Labels:** aggregator, field\n",
		"- **Created:** 2026-10-13 17:04 -0400",
		"- **Time spent:** 30m",
		"## Fields\n\n- **Product:** MCP Data Aggregator, Connect\n- **Software Version:** 2.4.1-dev\n",
		"### Steps to Reproduce\n\n1\\. Stop the gateway\n\n2\\. Watch it hang\n",
		"[attachment: stop-trace.png](https://acme.atlassian.net/rest/api/3/attachment/content/10001)",
		"\n[attachment]\n",
		"## Links\n\n- blocks CSM-700: Gateway restart (Open)\n- relates to MCP-7401: Driver timeouts (Done)\n",
		"## Attachments\n\n- [stop-trace.png](https://acme.atlassian.net/rest/api/3/attachment/content/10001) (2.0 KB, image/png, Mia Krystof, 2026-10-13 17:05 -0400)\n- [aggregator.log](https://acme.atlassian.net/rest/api/3/attachment/content/10002) (512 B, text/plain, 2026-10-13 17:06 -0400)\n",
		"## Comments (newest 2 of 12)\n\n### Mia Krystof · 2026-10-14 08:00 -0400\n\nCustomer reply underlined\n\n### Tony Okeke · 2026-10-15 09:00 -0400 · internal\n\nInternal: driver issue.\n",
		"## History\n\n- 2026-10-14 10:00 -0400 Tony Okeke: status: Open → In Progress; assignee: — → Tony Okeke\n",
	)
	for _, hidden := range []string{"Rank", "Time to resolution", "Development", "Empty", "media m-unknown"} {
		if strings.Contains(res.Text, hidden) {
			t.Errorf("view shows %q:\n%s", hidden, res.Text)
		}
	}
	issueReq := fake.Find("GET /rest/api/3/issue/CSM-698")[0]
	q, _ := url.ParseQuery(issueReq.Query)
	if q.Get("fields") != "*all,-comment,-worklog" || q.Get("expand") != "names,schema,renderedFields" {
		t.Errorf("issue query = %s", issueReq.Query)
	}
	commentReq := fake.Find("GET /rest/api/3/issue/CSM-698/comment")[0]
	q, _ = url.ParseQuery(commentReq.Query)
	if q.Get("orderBy") != "-created" || q.Get("maxResults") != "2" || q.Get("expand") != "renderedBody" {
		t.Errorf("comment query = %s", commentReq.Query)
	}
}

func TestViewDefaultsAndNoHistory(t *testing.T) {
	fake := viewFake()
	res := mustInvoke(t, fake, View, `{"issue":"CSM-698"}`)
	if strings.Contains(res.Text, "## History") || len(fake.Find("GET /rest/api/3/issue/CSM-698/changelog")) != 0 {
		t.Error("history fetched or shown without history: true")
	}
	q, _ := url.ParseQuery(fake.Find("GET /rest/api/3/issue/CSM-698/comment")[0].Query)
	if q.Get("maxResults") != "10" {
		t.Errorf("default comment page = %s", q.Get("maxResults"))
	}
}

func TestViewErrors(t *testing.T) {
	fake := jiratest.New()
	_, err := invoke(t, fake, View, `{"issue":"not a key"}`)
	wantKind(t, err, ops.Invalid, "expected an issue key")
	_, err = invoke(t, fake, View, `{"issue":"CSM-404"}`)
	wantKind(t, err, ops.NotFound)
	_, err = invoke(t, fake, View, `{"issue":"CSM-1","extra":true}`)
	wantKind(t, err, ops.Invalid, "unknown field")
}

func TestIssueKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"CSM-1", "CSM-1"},
		{" csm-12 ", "CSM-12"},
		{"10042", "10042"},
		{"https://acme.atlassian.net/browse/CSM-9", "CSM-9"},
		{"https://acme.atlassian.net/jira/servicedesk/projects/CSM/queues/custom/1/CSM-9", ""},
		{"https://acme.atlassian.net/jira/software/projects/MCP/boards/1?selectedIssue=MCP-7", "MCP-7"},
		{"CSM-1/../admin", ""},
		{"CSM", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got, err := issueKey("issue", tc.in)
		if tc.want == "" {
			if err == nil {
				t.Errorf("issueKey(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("issueKey(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestViewListsSubtasks(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-1", http.StatusOK, `{"id":"1","key":"CSM-1","fields":{"summary":"Story",
		"issuetype":{"name":"Story","hierarchyLevel":0},
		"subtasks":[{"key":"CSM-2","fields":{"summary":"Write  tests","status":{"name":"Open"},"issuetype":{"name":"Subtask","subtask":true}}}]}}`)
	fake.JSON("GET /rest/api/3/issue/CSM-1/comment", http.StatusOK, `{"startAt":0,"total":0,"comments":[]}`)
	res := mustInvoke(t, fake, View, `{"issue":"CSM-1"}`)
	wantContains(t, res.Text, "\n## Subtasks\n\n- CSM-2 [Subtask] Write tests (Open)\n")
	if strings.Contains(res.Text, "Child issues") || len(fake.Find("POST /rest/api/3/search/jql")) != 0 {
		t.Errorf("a story's view searched for children:\n%s", res.Text)
	}
}

func TestViewListsAnEpicsChildren(t *testing.T) {
	epic := func() *jiratest.Fake {
		fake := jiratest.New()
		fake.JSON("GET /rest/api/3/issue/MCP-12", http.StatusOK, `{"id":"12","key":"MCP-12","fields":{"summary":"Epic","issuetype":{"name":"Epic","hierarchyLevel":1}}}`)
		fake.JSON("GET /rest/api/3/issue/MCP-12/comment", http.StatusOK, `{"startAt":0,"total":0,"comments":[]}`)
		return fake
	}
	fake := epic()
	fake.JSON("POST /rest/api/3/search/jql", http.StatusOK, `{"issues":[
		{"key":"MCP-13","fields":{"summary":"Export","status":{"name":"Done"},"issuetype":{"name":"Story"},"assignee":{"accountId":"a","displayName":"Mia"}}},
		{"key":"MCP-14","fields":{"summary":"Import","status":{"name":"Open"},"issuetype":{"name":"Task"}}}],"nextPageToken":"t2"}`)
	res := mustInvoke(t, fake, View, `{"issue":"MCP-12"}`)
	wantContains(t, res.Text,
		"\n## Child issues\n\n- MCP-13 [Story] Export (Done) · Mia\n- MCP-14 [Task] Import (Open)\n",
		"Showing the first 2; search parent = MCP-12 for the rest.")
	search := body(t, fake.Find("POST /rest/api/3/search/jql")[0])
	if search["jql"] != "parent = MCP-12 ORDER BY created ASC" || search["maxResults"] != float64(50) {
		t.Errorf("search = %v", search)
	}

	fake = epic()
	fake.JSON("POST /rest/api/3/search/jql", http.StatusBadRequest, `{"errorMessages":["Field 'parent' does not exist"]}`)
	res = mustInvoke(t, fake, View, `{"issue":"MCP-12"}`)
	wantContains(t, res.Text, "## Child issues\n\nCould not list them (", "search parent = MCP-12.")
}
