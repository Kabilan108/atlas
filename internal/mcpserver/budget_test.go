package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kabilan108/atlas/internal/mcpserver"
)

// Every tool's tools/list entry is sent to the model on every turn, so each
// has a byte budget and so does the whole list. Keep descriptions useful
// rather than trimming them to pass: raise a budget here, by name, instead.
const (
	toolBudget  = 1024
	totalBudget = 40 * 1024
)

// toolBudgetExceptions are tools whose nested inputs (comment and field
// lists) need more room. They apply once the tool exists.
var toolBudgetExceptions = map[string]int{
	"pr_review":         1434,
	"pr_create":         1434,
	"jira_issue_create": 1434,
	// pr_list's scope (repo, repos, workspace/*), filters, and limit.
	"pr_list": 1152,
	// pr_comment's line says where to read line numbers.
	"pr_comment": 1152,
}

func TestToolsListBudget(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	result, err := connect(t, srv.url, nil).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, tool := range result.Tools {
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		size := len(data)
		total += size
		budget := toolBudget
		if exception, ok := toolBudgetExceptions[tool.Name]; ok {
			budget = exception
			if size <= toolBudget {
				t.Logf("%s fits the default budget; its exception can go", tool.Name)
			}
		}
		t.Logf("%s: %d of %d bytes", tool.Name, size, budget)
		if size > budget {
			t.Errorf("%s is %d bytes in tools/list, over its %d-byte budget:\n%s", tool.Name, size, budget, data)
		}
	}
	t.Logf("total: %d of %d bytes across %d tools", total, totalBudget, len(result.Tools))
	if total > totalBudget {
		t.Errorf("tools/list is %d bytes, over the %d-byte total budget", total, totalBudget)
	}
}
