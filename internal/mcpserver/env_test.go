package mcpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testJiraToken = "fake-jira-token-91b2"

type noInput struct{}

// serveDefs serves defs over stateless Streamable HTTP, so calls carry
// headers.
func serveDefs(t *testing.T, opts mcpserver.Options, defs ...ops.Def) string {
	t.Helper()
	server, err := mcpserver.NewServer(opts, defs)
	if err != nil {
		t.Fatal(err)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// jiraWhoami asks the Env for the Jira client twice, which must give the same
// client, then reads /myself.
var jiraWhoami = &ops.Op[noInput, string]{
	Meta: ops.Meta{Name: "jira_whoami_test", Description: "Test.", Surfaces: ops.MCP, ReadOnly: true},
	Run: func(ctx context.Context, env ops.Env, _ noInput) (string, error) {
		first, err := env.Jira(ctx)
		if err != nil {
			return "", err
		}
		second, err := env.Jira(ctx)
		if err != nil {
			return "", err
		}
		if first != second {
			return "", ops.Errorf(ops.Unknown, "Jira client rebuilt within one call")
		}
		user, err := first.Myself(ctx)
		if err != nil {
			return "", err
		}
		return user.DisplayName, nil
	},
}

var bitbucketWhoami = &ops.Op[noInput, string]{
	Meta: ops.Meta{Name: "bitbucket_whoami_test", Description: "Test.", Surfaces: ops.MCP, ReadOnly: true},
	Run: func(ctx context.Context, env ops.Env, _ noInput) (string, error) {
		client, err := env.Bitbucket(ctx)
		if err != nil {
			return "", err
		}
		user, err := client.GetCurrentUser(ctx)
		if err != nil {
			return "", err
		}
		return user.DisplayName, nil
	},
}

func statusServer(t *testing.T, status int, body string, seen *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, _ := r.BasicAuth()
		*seen = append(*seen, user+":"+password)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestJiraClientComesFromItsOwnHeader(t *testing.T) {
	var seen []string
	jiraAPI := statusServer(t, http.StatusOK, `{"accountId":"a1","displayName":"Ada"}`, &seen)
	url := serveDefs(t, mcpserver.Options{JiraOptions: []jira.Option{jira.WithBaseURL(jiraAPI.URL)}}, jiraWhoami)

	bitbucketOnly := connect(t, url, credentials())
	text, isError := callText(t, bitbucketOnly, "jira_whoami_test", nil)
	if !isError || text != "auth: missing required header "+mcpserver.HeaderJiraToken {
		t.Fatalf("without the Jira header: isError=%v text=%q", isError, text)
	}

	header := credentials()
	header.Set(mcpserver.HeaderJiraToken, testJiraToken)
	text, isError = callText(t, connect(t, url, header), "jira_whoami_test", nil)
	if isError || text != `"Ada"` {
		t.Fatalf("with the Jira header: isError=%v text=%q", isError, text)
	}
	if len(seen) != 1 || seen[0] != testEmail+":"+testJiraToken {
		t.Fatalf("Jira saw credentials %v, want the email and Jira token once", seen)
	}
}

func TestAuthErrorsNameTheProductHeader(t *testing.T) {
	var seen []string
	tests := []struct {
		name   string
		status int
		op     ops.Def
		want   []string
	}{
		{"jira 401", http.StatusUnauthorized, jiraWhoami, []string{"auth: jira: GET /rest/api/3/myself: 401", "Hint: check the X-Atlas-Email and X-Atlas-Jira-Token headers"}},
		{"jira 403", http.StatusForbidden, jiraWhoami, []string{"auth: ", "Hint: the token in X-Atlas-Jira-Token may lack a scope"}},
		{"bitbucket 401", http.StatusUnauthorized, bitbucketWhoami, []string{"auth: authentication: invalid credentials\nHint: check the X-Atlas-Email and X-Atlas-Bitbucket-Token headers"}},
		{"bitbucket 403", http.StatusForbidden, bitbucketWhoami, []string{"auth: authentication: access denied\nHint: the token in X-Atlas-Bitbucket-Token may lack a scope"}},
		{"bitbucket 409", http.StatusConflict, bitbucketWhoami, []string{"conflict: "}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := statusServer(t, tc.status, `{"errorMessages":["denied"]}`, &seen)
			url := serveDefs(t, mcpserver.Options{
				JiraOptions:      []jira.Option{jira.WithBaseURL(api.URL)},
				BitbucketOptions: []bitbucket.ClientOption{bitbucket.WithBaseURL(api.URL)},
			}, tc.op)
			header := credentials()
			header.Set(mcpserver.HeaderJiraToken, testJiraToken)
			text, isError := callText(t, connect(t, url, header), tc.op.Info().Name, nil)
			if !isError {
				t.Fatalf("call succeeded: %s", text)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("text = %q, want it to contain %q", text, want)
				}
			}
			if strings.Contains(text, "atlas config") || strings.Contains(text, "atlas login") {
				t.Fatalf("MCP error carries a CLI hint: %q", text)
			}
		})
	}
}
