package jira

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

const createMetaTypes = `{"startAt":0,"maxResults":50,"total":2,"isLast":true,"issueTypes":[
	{"id":"10010","name":"Bug"},{"id":"10011","name":"Task"}]}`

const createMetaFields = `{"startAt":0,"maxResults":50,"total":7,"isLast":true,"fields":[
	{"fieldId":"summary","name":"Summary","required":true,"schema":{"type":"string","system":"summary"}},
	{"fieldId":"description","name":"Description","required":false,"schema":{"type":"string","system":"description"}},
	{"fieldId":"priority","name":"Priority","required":false,"schema":{"type":"priority","system":"priority"},
	 "allowedValues":[{"id":"1","name":"Highest"},{"id":"2","name":"High"}]},
	{"fieldId":"customfield_10153","name":"Product","required":true,
	 "schema":{"type":"array","items":"option","custom":"com.atlassian.jira.plugin.system.customfieldtypes:multiselect"},
	 "allowedValues":[{"id":"10237","value":"MCP Data Aggregator"},{"id":"10238","value":"Connect"}]},
	{"fieldId":"customfield_10469","name":"Software Version","required":false,
	 "schema":{"type":"string","custom":"com.atlassian.jira.plugin.system.customfieldtypes:textfield"}},
	{"fieldId":"customfield_10500","name":"Reviewer","required":false,"schema":{"type":"user","custom":"com.atlassian.jira.plugin.system.customfieldtypes:userpicker"}},
	{"fieldId":"labels","name":"Labels","required":false,"schema":{"type":"array","items":"string","system":"labels"}}]}`

func createFake() *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes", http.StatusOK, createMetaTypes)
	fake.JSON("GET /rest/api/3/issue/createmeta/CSM/issuetypes/10010", http.StatusOK, createMetaFields)
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me","displayName":"Agent","active":true}`)
	fake.JSON("GET /rest/api/3/user/assignable/search", http.StatusOK, `[{"accountId":"acc-mia","displayName":"Mia Krystof","emailAddress":"mia@example.org","active":true}]`)
	fake.JSON("POST /rest/api/3/issue", http.StatusCreated, `{"id":"30001","key":"CSM-757","self":"x"}`)
	return fake
}

func TestCreateShapesFieldsByName(t *testing.T) {
	fake := createFake()
	res := mustInvoke(t, fake, Create, `{"project":"csm","type":"bug","summary":"Gateway stuck","body":"## Steps\n\n- [ ] restart",
		"priority":"high","assignee":"me","parent":"CSM-600","labels":["field"],
		"fields":{"product":["MCP Data Aggregator","connect"],"Software Version":"2.4.1","Reviewer":"mia@example.org"}}`)
	if res.Text != `{"key":"CSM-757","id":"30001","url":"https://acme.atlassian.net/browse/CSM-757"}` {
		t.Errorf("receipt = %s", res.Text)
	}
	posts := fake.Find("POST /rest/api/3/issue")
	if len(posts) != 1 {
		t.Fatalf("posts = %d", len(posts))
	}
	fields := body(t, posts[0])["fields"].(map[string]any)
	for id, want := range map[string]string{
		"project":           `{"key":"CSM"}`,
		"issuetype":         `{"id":"10010"}`,
		"summary":           `"Gateway stuck"`,
		"priority":          `{"name":"High"}`,
		"assignee":          `{"accountId":"acc-me"}`,
		"parent":            `{"key":"CSM-600"}`,
		"labels":            `["field"]`,
		"customfield_10153": `[{"value":"MCP Data Aggregator"},{"value":"Connect"}]`,
		"customfield_10469": `"2.4.1"`,
		"customfield_10500": `{"accountId":"acc-mia"}`,
	} {
		if got := jsonOf(t, fields[id]); got != want {
			t.Errorf("fields.%s = %s, want %s", id, got, want)
		}
	}
	if desc := jsonOf(t, fields["description"]); !strings.Contains(desc, `"type":"taskList"`) || !strings.Contains(desc, `"type":"heading"`) {
		t.Errorf("description = %s", desc)
	}
}

func TestCreateResolverMisses(t *testing.T) {
	tests := []struct {
		name string
		args string
		want []string
	}{
		{"type", `{"project":"CSM","type":"Epic","summary":"x"}`, []string{"no issue type \"Epic\"", "valid: Bug, Task"}},
		{"field", `{"project":"CSM","type":"Bug","summary":"x","fields":{"Nope":"y"}}`, []string{`no field "Nope"`, "Product", "Software Version"}},
		{"option", `{"project":"CSM","type":"Bug","summary":"x","fields":{"Product":["Widget"]}}`, []string{`no value for Product "Widget"`, "Connect, MCP Data Aggregator"}},
		{"priority", `{"project":"CSM","type":"Bug","summary":"x","priority":"Low"}`, []string{`"Low"`, "High, Highest"}},
		{"twice", `{"project":"CSM","type":"Bug","summary":"x","fields":{"summary":"y"}}`, []string{"Summary (summary) is set twice"}},
		{"project", `{"project":"C SM","type":"Bug","summary":"x"}`, []string{"invalid project"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := createFake()
			_, err := invoke(t, fake, Create, tc.args)
			wantKind(t, err, ops.Invalid, tc.want...)
			noWrites(t, fake)
		})
	}
}

func TestCreateNamesFieldsInJiraErrors(t *testing.T) {
	fake := createFake()
	fake.JSON("POST /rest/api/3/issue", http.StatusBadRequest, `{"errorMessages":[],"errors":{"customfield_10153":"Product is required."}}`)
	_, err := invoke(t, fake, Create, `{"project":"CSM","type":"Bug","summary":"x"}`)
	wantKind(t, err, ops.Invalid, "customfield_10153: Product is required.", "customfield_10153 is Product (allowed: MCP Data Aggregator, Connect)")
}

const richDescription = `{"type":"doc","version":1,"content":[
	{"type":"paragraph","content":[{"type":"text","text":"keep <me> & \"this\"","marks":[{"type":"textColor","attrs":{"color":"#ff5630"}}]}]},
	{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"m1","type":"file","collection":""}}]}]}`

func editFake(description string) *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-756", http.StatusOK, `{"id":"1","key":"CSM-756","fields":{"description":`+description+`}}`)
	fake.JSON("PUT /rest/api/3/issue/CSM-756", http.StatusNoContent, ``)
	fake.JSON("GET /rest/api/3/issue/CSM-756/editmeta", http.StatusOK, `{"fields":{
		"customfield_10469":{"name":"Software Version","required":false,"schema":{"type":"string"}},
		"customfield_10153":{"name":"Product","required":true,"schema":{"type":"array","items":"option"},"allowedValues":[{"value":"Connect"}]}}}`)
	fake.JSON("GET /rest/api/3/user/assignable/search", http.StatusOK, `[{"accountId":"acc-1","displayName":"Ana Lee","active":true},{"accountId":"acc-2","displayName":"Ana Lima","active":true}]`)
	return fake
}

func TestEditAppendKeepsExistingADF(t *testing.T) {
	fake := editFake(richDescription)
	res := mustInvoke(t, fake, Edit, `{"issue":"CSM-756","body":"Follow-up: **done**","body_mode":"append","labels":["triaged","-stale"],"assignee":"none"}`)
	if res.Text != `{"key":"CSM-756","url":"https://acme.atlassian.net/browse/CSM-756","changed":["description (appended)","labels","assignee"]}` {
		t.Errorf("receipt = %s", res.Text)
	}
	put := fake.Find("PUT /rest/api/3/issue/CSM-756")[0]
	got := body(t, put)
	var original map[string]any
	if err := json.Unmarshal([]byte(richDescription), &original); err != nil {
		t.Fatal(err)
	}
	blocks := got["fields"].(map[string]any)["description"].(map[string]any)["content"].([]any)
	want := original["content"].([]any)
	if len(blocks) != len(want)+1 || jsonOf(t, blocks[:len(want)]) != jsonOf(t, want) {
		t.Errorf("existing blocks changed:\n%s", put.Body)
	}
	if last := jsonOf(t, blocks[len(want)]); !strings.Contains(last, `"marks":[{"type":"strong"}],"text":"done"`) {
		t.Errorf("appended block = %s", last)
	}
	if jsonOf(t, got["update"]) != `{"labels":[{"add":"triaged"},{"remove":"stale"}]}` {
		t.Errorf("update = %s", jsonOf(t, got["update"]))
	}
	if fields := got["fields"].(map[string]any); fields["assignee"] != nil {
		t.Errorf("assignee = %v, want null", fields["assignee"])
	} else if _, ok := fields["assignee"]; !ok {
		t.Error("assignee missing; want explicit null")
	}
}

func TestEditReplaceRefusesLossyDescription(t *testing.T) {
	fake := editFake(richDescription)
	_, err := invoke(t, fake, Edit, `{"issue":"CSM-756","body":"new text"}`)
	wantKind(t, err, ops.Invalid, "media x1, text color x1", "body_mode append", "force")
	noWrites(t, fake)

	fake = editFake(richDescription)
	mustInvoke(t, fake, Edit, `{"issue":"CSM-756","body":"new text","force":true}`)
	if len(fake.Find("GET /rest/api/3/issue/CSM-756")) != 0 {
		t.Error("force still read the description")
	}
	desc := jsonOf(t, body(t, fake.Find("PUT /rest/api/3/issue/CSM-756")[0])["fields"].(map[string]any)["description"])
	if strings.Contains(desc, "media") || !strings.Contains(desc, "new text") {
		t.Errorf("forced description = %s", desc)
	}
}

func TestEditReplaceSimpleDescription(t *testing.T) {
	fake := editFake(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"old"}]}]}`)
	mustInvoke(t, fake, Edit, `{"issue":"CSM-756","body":"new","summary":"Renamed","fields":{"software version":"3.0","Product":["connect"]}}`)
	fields := body(t, fake.Find("PUT /rest/api/3/issue/CSM-756")[0])["fields"].(map[string]any)
	if fields["summary"] != "Renamed" || fields["customfield_10469"] != "3.0" || jsonOf(t, fields["customfield_10153"]) != `[{"value":"Connect"}]` {
		t.Errorf("fields = %s", jsonOf(t, fields))
	}
}

func TestEditErrors(t *testing.T) {
	tests := []struct {
		name, args string
		want       []string
	}{
		{"nothing", `{"issue":"CSM-756"}`, []string{"nothing to change"}},
		{"mode without body", `{"issue":"CSM-756","body_mode":"append","summary":"x"}`, []string{"only with body"}},
		{"bad mode", `{"issue":"CSM-756","body":"x","body_mode":"prepend"}`, []string{"replace, append"}},
		{"ambiguous assignee", `{"issue":"CSM-756","assignee":"Ana"}`, []string{"ambiguous", "Ana Lee (acc-1)", "Ana Lima (acc-2)"}},
		{"bad label", `{"issue":"CSM-756","labels":["two words"]}`, []string{"invalid label"}},
		{"unknown field", `{"issue":"CSM-756","fields":{"Severity":"1"}}`, []string{`no field "Severity"`, "Product, Software Version"}},
		{"huge body", `{"issue":"CSM-756","body":"` + strings.Repeat("x", MaxBodyBytes+1) + `","force":true}`, []string{"over the 131072-byte limit"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := editFake(`null`)
			_, err := invoke(t, fake, Edit, tc.args)
			wantKind(t, err, ops.Invalid, tc.want...)
			noWrites(t, fake)
		})
	}
}

func TestSearchPagesAndResolvesFieldNames(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/field", http.StatusOK, `[{"id":"customfield_10153","name":"Product","custom":true},{"id":"summary","name":"Summary"}]`)
	var tokens []string
	fake.Handle("POST /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		req := map[string]any{}
		_ = jsonDecode(r, &req)
		token, _ := req["nextPageToken"].(string)
		tokens = append(tokens, token)
		if req["jql"] != "project = CSM AND text ~ \"a|b\" ORDER BY updated DESC" {
			t.Errorf("jql = %v", req["jql"])
		}
		if jsonOf(t, req["fields"]) != `["issuetype","status","priority","assignee","updated","summary","customfield_10153"]` {
			t.Errorf("fields = %s", jsonOf(t, req["fields"]))
		}
		switch token {
		case "":
			jiratest.Write(w, 200, `{"nextPageToken":"p2","issues":[{"key":"CSM-1","fields":{"summary":"One | pipe","status":{"name":"Open"},"issuetype":{"name":"Bug"},"updated":"2026-11-01T10:00:00.000-0400","customfield_10153":[{"value":"Connect"}]}}]}`)
		case "p2":
			jiratest.Write(w, 200, `{"nextPageToken":"p3","issues":[{"key":"CSM-2","fields":{"summary":"Two","assignee":{"displayName":"Mia"}}}]}`)
		default:
			t.Errorf("fetched past the limit with token %q", token)
			jiratest.Write(w, 200, `{"issues":[]}`)
		}
	})
	res := mustInvoke(t, fake, Search, `{"jql":"project = CSM AND text ~ \"a|b\" ORDER BY updated DESC","fields":["product"],"limit":2}`)
	want := "| Key | Type | Status | Priority | Assignee | Updated | Summary | Product |\n" +
		"| --- | --- | --- | --- | --- | --- | --- | --- |\n" +
		"| CSM-1 | Bug | Open | — | — | 2026-11-01 | One \\| pipe | Connect |\n" +
		"| CSM-2 | — | — | — | Mia | — | Two | — |\n" +
		"\n2 issues. More match: call again with page_token \"p3\".\n"
	if res.Text != want {
		t.Errorf("search =\n%s\nwant\n%s", res.Text, want)
	}
	if strings.Join(tokens, ",") != ",p2" {
		t.Errorf("tokens = %v", tokens)
	}
}

func TestSearchErrors(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/field", http.StatusOK, `[{"id":"summary","name":"Summary"}]`)
	_, err := invoke(t, fake, Search, `{"jql":"project = CSM","fields":["Nope"]}`)
	wantKind(t, err, ops.Invalid, `no field "Nope"`)
	_, err = invoke(t, fake, Search, `{"jql":" "}`)
	wantKind(t, err, ops.Invalid, "jql is required")
	for _, limit := range []string{"-1", "1001"} {
		_, err = invoke(t, fake, Search, `{"jql":"x","limit":`+limit+`}`)
		wantKind(t, err, ops.Invalid, "between 1 and 1000")
	}
	fake.JSON("POST /rest/api/3/search/jql", http.StatusBadRequest, `{"errorMessages":["Error in the JQL Query"]}`)
	_, err = invoke(t, fake, Search, `{"jql":"project = = CSM"}`)
	wantKind(t, err, ops.Invalid, "Error in the JQL Query")
}

func TestSearchNoResults(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("POST /rest/api/3/search/jql", http.StatusOK, `{"issues":[],"isLast":true}`)
	if res := mustInvoke(t, fake, Search, `{"jql":"project = CSM"}`); res.Text != "No issues match.\n" {
		t.Errorf("text = %q", res.Text)
	}
}
