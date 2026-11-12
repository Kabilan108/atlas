package mcpserver_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/mcpserver"
)

// The api tool's path template must not carry what the path named: here a
// Jira user property keyed by an encoded email, with an account ID query.
func TestAPIPathLogsNeverRecordPathValues(t *testing.T) {
	var seen []string
	jiraAPI := statusServer(t, http.StatusOK, `{"key":"x","value":{}}`, &seen)
	logs := &syncBuffer{}
	handler, err := mcpserver.Handler(mcpserver.Options{
		Logger:      slog.New(slog.NewJSONHandler(logs, nil)),
		JiraOptions: []jira.Option{jira.WithBaseURL(jiraAPI.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	header := credentials()
	header.Set(mcpserver.HeaderJiraToken, testJiraToken)
	session := connect(t, server.URL+"/mcp", header)
	path := "/rest/api/3/user/properties/alice.private%40example.invalid?accountId=557058:0f1e2d3c"
	if text, isError := callText(t, session, "api", map[string]any{"path": path}); isError {
		t.Fatal(text)
	}
	session.Close()
	server.Close()

	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if apiPath, ok := entry["api_path"].(string); ok {
			paths = append(paths, apiPath)
		}
	}
	if len(paths) != 1 || paths[0] != "GET /rest/api/3/user/properties/{value}" {
		t.Fatalf("api_path entries = %q", paths)
	}
	for _, value := range []string{"alice", "example.invalid", "557058", "0f1e2d3c"} {
		if strings.Contains(logs.String(), value) {
			t.Fatalf("logs record %q:\n%s", value, logs.String())
		}
	}
}
