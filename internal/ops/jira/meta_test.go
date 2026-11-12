package jira

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

// hugePages serves offset pages from a list of size items that Jira never
// marks last, counting what it served.
func hugePages(listKey string, size int, served *int, item func(i int) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.Atoi(q.Get("startAt"))
		limit, _ := strconv.Atoi(q.Get("maxResults"))
		var items []string
		for i := start; i < start+limit && i < size; i++ {
			items = append(items, item(i))
		}
		*served += len(items)
		jiratest.Write(w, http.StatusOK, fmt.Sprintf(`{"startAt":%d,"maxResults":%d,"total":%d,"isLast":false,"%s":[%s]}`,
			start, limit, size, listKey, strings.Join(items, ",")))
	}
}

func hugeMetaFake(types, fields int, servedTypes, servedFields *int) *jiratest.Fake {
	fake := createFake()
	fake.Handle("GET /rest/api/3/issue/createmeta/CSM/issuetypes", hugePages("issueTypes", types, servedTypes, func(i int) string {
		if i == 0 {
			return `{"id":"10010","name":"Bug"}`
		}
		return fmt.Sprintf(`{"id":"%d","name":"Type %d"}`, 20000+i, i)
	}))
	fake.Handle("GET /rest/api/3/issue/createmeta/CSM/issuetypes/10010", hugePages("fields", fields, servedFields, func(i int) string {
		return fmt.Sprintf(`{"fieldId":"customfield_%d","name":"Field %d","schema":{"type":"string"}}`, 30000+i, i)
	}))
	return fake
}

func TestCreateMetaStopsAtItsMaximum(t *testing.T) {
	t.Run("fields", func(t *testing.T) {
		var types, fields int
		fake := hugeMetaFake(1, 10000, &types, &fields)
		res := mustInvoke(t, fake, Fields, `{"project":"CSM","type":"Bug"}`)
		if fields > MaxCreateMetaFields+1 {
			t.Errorf("read %d fields for a maximum of %d", fields, MaxCreateMetaFields)
		}
		out := res.Value.(*FieldsResult)
		if len(out.Fields) != MaxCreateMetaFields || !out.Truncated {
			t.Errorf("listed %d fields, truncated %v", len(out.Fields), out.Truncated)
		}
		wantContains(t, res.Text, "first 2000 fields")
	})
	t.Run("issue types", func(t *testing.T) {
		var types, fields int
		fake := hugeMetaFake(10000, 1, &types, &fields)
		res := mustInvoke(t, fake, Fields, `{"project":"CSM"}`)
		if types > MaxIssueTypes+1 {
			t.Errorf("read %d issue types for a maximum of %d", types, MaxIssueTypes)
		}
		wantContains(t, res.Text, "first 500 issue types")
	})
}

// A write that names a field or type past the metadata atlas read refuses,
// and says the metadata was cut off rather than that the name is wrong.
func TestCreateRefusesNamesPastTruncatedMetadata(t *testing.T) {
	var types, fields int
	fake := hugeMetaFake(1, 10000, &types, &fields)
	_, err := invoke(t, fake, Create, `{"project":"CSM","type":"Bug","summary":"x","fields":{"Field 5000":"v"}}`)
	wantKind(t, err, ops.Invalid, `"Field 5000"`, "first 2000 fields", "Jira has more")
	noWrites(t, fake)

	fake = hugeMetaFake(1, 10000, &types, &fields)
	mustInvoke(t, fake, Create, `{"project":"CSM","type":"Bug","summary":"x","fields":{"Field 7":"v"}}`)

	fake = hugeMetaFake(10000, 1, &types, &fields)
	_, err = invoke(t, fake, Create, `{"project":"CSM","type":"Type 9000","summary":"x"}`)
	wantKind(t, err, ops.Invalid, `"Type 9000"`, "first 500 issue types", "Jira has more")
	noWrites(t, fake)
}
