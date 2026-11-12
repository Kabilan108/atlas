package jira

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

const transitionsJSON = `{"transitions":[
	{"id":"11","name":"Start Progress","to":{"name":"In Progress"}},
	{"id":"31","name":"Resolved (2)","to":{"name":"Resolved"},"fields":{
		"resolution":{"name":"Resolution","required":true,"schema":{"type":"resolution","system":"resolution"},
		              "allowedValues":[{"id":"1","name":"Done"},{"id":"2","name":"Won't Do"}]}}},
	{"id":"41","name":"Close","to":{"name":"Closed"}}]}`

func transitionFake() *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-756/transitions", http.StatusOK, transitionsJSON)
	fake.JSON("POST /rest/api/3/issue/CSM-756/transitions", http.StatusNoContent, ``)
	return fake
}

func TestTransitionsList(t *testing.T) {
	res := mustInvoke(t, transitionFake(), Transitions, `{"issue":"CSM-756"}`)
	wantContains(t, res.Text,
		"| Start Progress | In Progress | 11 | — |",
		"| Resolved (2) | Resolved | 31 | Resolution (Done, Won't Do) |")
}

func TestTransitionByNameOrStatus(t *testing.T) {
	for _, name := range []string{"resolved", "RESOLVED (2)", "31"} {
		fake := transitionFake()
		res := mustInvoke(t, fake, Transition, `{"issue":"CSM-756","transition":"`+name+`","fields":{"resolution":"done"}}`)
		if res.Text != `{"key":"CSM-756","transition":"Resolved (2)","status":"Resolved","url":"https://acme.atlassian.net/browse/CSM-756"}` {
			t.Errorf("%s: receipt = %s", name, res.Text)
		}
		got := body(t, fake.Find("POST /rest/api/3/issue/CSM-756/transitions")[0])
		if jsonOf(t, got) != `{"fields":{"resolution":{"name":"Done"}},"transition":{"id":"31"}}` {
			t.Errorf("%s: body = %s", name, jsonOf(t, got))
		}
	}
	fake := transitionFake()
	mustInvoke(t, fake, Transition, `{"issue":"CSM-756","transition":"in progress"}`)
	if got := jsonOf(t, body(t, fake.Find("POST /rest/api/3/issue/CSM-756/transitions")[0])); got != `{"transition":{"id":"11"}}` {
		t.Errorf("by status: %s", got)
	}
}

func TestTransitionMissListsValidNames(t *testing.T) {
	fake := transitionFake()
	_, err := invoke(t, fake, Transition, `{"issue":"CSM-756","transition":"Reopen"}`)
	wantKind(t, err, ops.Invalid, `no transition "Reopen"`, "Close (to Closed)", "Resolved (2) (to Resolved)", "Start Progress (to In Progress)")
	noWrites(t, fake)

	fake = transitionFake()
	_, err = invoke(t, fake, Transition, `{"issue":"CSM-756","transition":"Resolved","fields":{"Resolution":"Fixed"}}`)
	wantKind(t, err, ops.Invalid, `no value for Resolution "Fixed"`, "Done, Won't Do")
	noWrites(t, fake)
}

func TestTransitionRequiredFieldErrorNamesField(t *testing.T) {
	fake := transitionFake()
	fake.JSON("POST /rest/api/3/issue/CSM-756/transitions", http.StatusBadRequest, `{"errors":{"resolution":"Resolution is required."}}`)
	_, err := invoke(t, fake, Transition, `{"issue":"CSM-756","transition":"Resolved"}`)
	wantKind(t, err, ops.Invalid, "Resolution is required.", "resolution is Resolution (allowed: Done, Won't Do)")
}

func commentFake(projectType string) *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issue/CSM-756", http.StatusOK, `{"id":"1","key":"CSM-756","fields":{"project":{"id":"10036","key":"CSM","name":"Customer Service","projectTypeKey":"`+projectType+`"}}}`)
	fake.JSON("POST /rest/api/3/issue/CSM-756/comment", http.StatusCreated, `{"id":"15200","jsdPublic":false}`)
	return fake
}

func TestPublicComment(t *testing.T) {
	fake := commentFake("service_desk")
	fake.JSON("POST /rest/api/3/issue/CSM-756/comment", http.StatusCreated, `{"id":"15199","jsdPublic":true}`)
	res := mustInvoke(t, fake, Comment, `{"issue":"CSM-756","body":"Thanks, **fixed** in 2.4.1"}`)
	if res.Text != `{"key":"CSM-756","id":"15199","url":"https://acme.atlassian.net/browse/CSM-756?focusedCommentId=15199","internal":false}` {
		t.Errorf("receipt = %s", res.Text)
	}
	got := body(t, fake.Find("POST /rest/api/3/issue/CSM-756/comment")[0])
	if _, ok := got["properties"]; ok || !strings.Contains(jsonOf(t, got["body"]), `"type":"strong"`) {
		t.Errorf("comment body = %s", jsonOf(t, got))
	}
	_, err := invoke(t, fake, Comment, `{"issue":"CSM-756","body":"  "}`)
	wantKind(t, err, ops.Invalid, "body is empty")
}

func TestInternalComment(t *testing.T) {
	fake := commentFake("service_desk")
	res := mustInvoke(t, fake, InternalComment, `{"issue":"CSM-756","body":"Agent-only note"}`)
	if res.Text != `{"key":"CSM-756","id":"15200","url":"https://acme.atlassian.net/browse/CSM-756?focusedCommentId=15200","internal":true}` {
		t.Errorf("receipt = %s", res.Text)
	}
	got := body(t, fake.Find("POST /rest/api/3/issue/CSM-756/comment")[0])
	if jsonOf(t, got["properties"]) != `[{"key":"sd.public.comment","value":{"internal":true}}]` {
		t.Errorf("properties = %s", jsonOf(t, got["properties"]))
	}
	if len(fake.Find("POST /rest/servicedeskapi/request/CSM-756/comment")) != 0 {
		t.Error("used the JSM request route")
	}
}

func TestInternalCommentRefusesNonServiceDesk(t *testing.T) {
	fake := commentFake("software")
	_, err := invoke(t, fake, InternalComment, `{"issue":"CSM-756","body":"note"}`)
	wantKind(t, err, ops.Invalid, "a software project, not a service desk", "jira_comment")
	noWrites(t, fake)
}

func TestInternalCommentLooksUpMissingProjectType(t *testing.T) {
	fake := commentFake("")
	fake.JSON("GET /rest/api/3/project/CSM", http.StatusOK, `{"id":"10036","key":"CSM","projectTypeKey":"business"}`)
	_, err := invoke(t, fake, InternalComment, `{"issue":"CSM-756","body":"note"}`)
	wantKind(t, err, ops.Invalid, "a business project")
	noWrites(t, fake)
}

func TestInternalCommentFailsUnlessJiraConfirmsInternal(t *testing.T) {
	for name, response := range map[string]string{
		"public":  `{"id":"15201","jsdPublic":true}`,
		"missing": `{"id":"15201"}`,
	} {
		t.Run(name, func(t *testing.T) {
			fake := commentFake("service_desk")
			fake.JSON("POST /rest/api/3/issue/CSM-756/comment", http.StatusCreated, response)
			_, err := invoke(t, fake, InternalComment, `{"issue":"CSM-756","body":"note"}`)
			wantKind(t, err, ops.Upstream, "comment 15201", "MAY BE VISIBLE TO CUSTOMERS",
				"https://acme.atlassian.net/browse/CSM-756?focusedCommentId=15201")
			for _, r := range fake.Requests() {
				if r.Method == "DELETE" {
					t.Errorf("deleted the comment: %s %s", r.Method, r.Path)
				}
			}
		})
	}
}

func linkFake(readback string) *jiratest.Fake {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/issueLinkType", http.StatusOK, `{"issueLinkTypes":[
		{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},
		{"id":"10003","name":"2 Relates","inward":"relates to","outward":"relates to"}]}`)
	fake.JSON("POST /rest/api/3/issueLink", http.StatusCreated, ``)
	fake.JSON("GET /rest/api/3/issue/CSM-1", http.StatusOK, `{"key":"CSM-1","fields":{"issuelinks":[`+readback+`]}}`)
	return fake
}

func TestLinkDirections(t *testing.T) {
	fake := linkFake(`{"id":"9","type":{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":"CSM-2"}}`)
	res := mustInvoke(t, fake, Link, `{"issue":"CSM-1","type":"blocks","target":"csm-2"}`)
	if res.Text != `{"issue":"CSM-1","target":"CSM-2","type":"Blocks","link_id":"9","reads":"CSM-1 blocks CSM-2"}` {
		t.Errorf("receipt = %s", res.Text)
	}
	if got := jsonOf(t, body(t, fake.Find("POST /rest/api/3/issueLink")[0])); got != `{"inwardIssue":{"key":"CSM-2"},"outwardIssue":{"key":"CSM-1"},"type":{"name":"Blocks"}}` {
		t.Errorf("outward body = %s", got)
	}

	fake = linkFake(`{"id":"10","type":{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"CSM-2"}}`)
	res = mustInvoke(t, fake, Link, `{"issue":"CSM-1","type":"Blocks","target":"CSM-2","direction":"inward"}`)
	wantContains(t, res.Text, `"reads":"CSM-1 is blocked by CSM-2"`)
	if got := jsonOf(t, body(t, fake.Find("POST /rest/api/3/issueLink")[0])); got != `{"inwardIssue":{"key":"CSM-1"},"outwardIssue":{"key":"CSM-2"},"type":{"name":"Blocks"}}` {
		t.Errorf("inward body = %s", got)
	}
}

func TestLinkTypeIgnoresNumericPrefix(t *testing.T) {
	fake := linkFake(`{"id":"11","type":{"id":"10003","name":"2 Relates","inward":"relates to","outward":"relates to"},"outwardIssue":{"key":"CSM-2"}}`)
	res := mustInvoke(t, fake, Link, `{"issue":"CSM-1","type":"relates","target":"CSM-2"}`)
	wantContains(t, res.Text, `"type":"2 Relates"`, `"reads":"CSM-1 relates to CSM-2"`)
}

func TestLinkErrors(t *testing.T) {
	fake := linkFake(``)
	_, err := invoke(t, fake, Link, `{"issue":"CSM-1","type":"Duplicates","target":"CSM-2"}`)
	wantKind(t, err, ops.Invalid, `no link type "Duplicates"`, "2 Relates, Blocks")
	noWrites(t, fake)
	_, err = invoke(t, fake, Link, `{"issue":"CSM-1","type":"Blocks","target":"CSM-1"}`)
	wantKind(t, err, ops.Invalid, "to itself")
	_, err = invoke(t, fake, Link, `{"issue":"CSM-1","type":"Blocks","target":"CSM-2","direction":"sideways"}`)
	wantKind(t, err, ops.Invalid, "outward, inward")

	fake = linkFake(`{"id":"9","type":{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"CSM-2"}}`)
	_, err = invoke(t, fake, Link, `{"issue":"CSM-1","type":"Blocks","target":"CSM-2"}`)
	wantKind(t, err, ops.Upstream, `do not show "CSM-1 blocks CSM-2"`)
}

func TestAssign(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/user/assignable/search", http.StatusOK, `[
		{"accountId":"acc-1","displayName":"Mia Krystof","emailAddress":"mia@example.org","active":true},
		{"accountId":"acc-2","displayName":"Mia Kim","active":true},
		{"accountId":"acc-3","displayName":"Mia Old","active":false}]`)
	fake.JSON("PUT /rest/api/3/issue/CSM-756/assignee", http.StatusNoContent, ``)
	res := mustInvoke(t, fake, Assign, `{"issue":"CSM-756","assignee":"MIA@example.org"}`)
	if res.Text != `{"key":"CSM-756","assignee":"Mia Krystof","account_id":"acc-1"}` {
		t.Errorf("receipt = %s", res.Text)
	}
	search := fake.Find("GET /rest/api/3/user/assignable/search")[0]
	if !strings.Contains(search.Query, "issueKey=CSM-756") || !strings.Contains(search.Query, "query=MIA%40example.org") {
		t.Errorf("search query = %s", search.Query)
	}
	mustInvoke(t, fake, Assign, `{"issue":"CSM-756","assignee":"mia kim"}`)
	_, err := invoke(t, fake, Assign, `{"issue":"CSM-756","assignee":"Mia"}`)
	wantKind(t, err, ops.Invalid, "ambiguous", "Mia Krystof (acc-1)", "Mia Kim (acc-2)")

	mustInvoke(t, fake, Assign, `{"issue":"CSM-756","assignee":"none"}`)
	puts := fake.Find("PUT /rest/api/3/issue/CSM-756/assignee")
	if got := puts[len(puts)-1].Body; got != `{"accountId":null}` {
		t.Errorf("unassign body = %s", got)
	}

	empty := jiratest.New()
	empty.JSON("GET /rest/api/3/user/assignable/search", http.StatusOK, `[]`)
	_, err = invoke(t, empty, Assign, `{"issue":"CSM-756","assignee":"nobody@example.org"}`)
	wantKind(t, err, ops.Invalid, "no active user assignable to CSM-756 matches")
}

func TestFields(t *testing.T) {
	fake := createFake()
	res := mustInvoke(t, fake, Fields, `{"project":"CSM","type":"bug"}`)
	wantContains(t, res.Text, "Fields for creating a CSM Bug:",
		"| Product | customfield_10153 | array of option (multiselect) | yes | MCP Data Aggregator, Connect |",
		"| Software Version | customfield_10469 | string (textfield) | no | — |")
	res = mustInvoke(t, fake, Fields, `{"project":"CSM"}`)
	if res.Text != "Issue types in CSM (give one as type for its fields):\n\n- Bug\n- Task\n" {
		t.Errorf("types = %q", res.Text)
	}
	fake.JSON("GET /rest/api/3/issue/CSM-756/editmeta", http.StatusOK, `{"fields":{"summary":{"name":"Summary","required":true,"schema":{"type":"string"}}}}`)
	res = mustInvoke(t, fake, Fields, `{"issue":"CSM-756"}`)
	wantContains(t, res.Text, "Fields CSM-756 can edit:", "| Summary | summary | string | yes | — |")
	for _, args := range []string{`{}`, `{"issue":"CSM-756","project":"CSM"}`} {
		_, err := invoke(t, fake, Fields, args)
		wantKind(t, err, ops.Invalid)
	}
}

func TestWhoamiAndProjects(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me","displayName":"Agent Smith","emailAddress":"agent@example.org","timeZone":"America/New_York","active":true}`)
	fake.JSON("GET /rest/api/3/project/search", http.StatusOK, `{"startAt":0,"maxResults":100,"total":2,"isLast":true,"values":[
		{"id":"1","key":"CSM","name":"Customer Service","projectTypeKey":"service_desk"},{"id":"2","key":"MCP","name":"MCP | Core","projectTypeKey":"software"}]}`)
	res := mustInvoke(t, fake, Whoami, `{}`)
	if res.Text != "**Agent Smith** (agent@example.org)\n\n- Account ID: acc-me\n- Time zone: America/New_York\n" {
		t.Errorf("whoami = %q", res.Text)
	}
	res = mustInvoke(t, fake, Projects, `{"query":"c"}`)
	if res.Text != "| Key | Name | Type |\n| --- | --- | --- |\n| CSM | Customer Service | service_desk |\n| MCP | MCP \\| Core | software |\n" {
		t.Errorf("projects = %q", res.Text)
	}
	if q := fake.Find("GET /rest/api/3/project/search")[0].Query; !strings.Contains(q, "query=c") || !strings.Contains(q, "maxResults=50") {
		t.Errorf("project query = %s", q)
	}
	_, err := invoke(t, fake, Projects, `{"limit":-1}`)
	wantKind(t, err, ops.Invalid, "between 1 and 1000")
}
