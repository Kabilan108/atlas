package pr_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops/pr/mergetest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type credentialTransport struct{}

func (credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(mcpserver.HeaderEmail, "agent@example.org")
	r.Header.Set(mcpserver.HeaderBitbucketToken, "fake-token")
	return http.DefaultTransport.RoundTrip(r)
}

func callMCPTool(t *testing.T, fixture *mergetest.Fixture, name string, args map[string]any) (string, bool) {
	t.Helper()
	handler, err := mcpserver.Handler(mcpserver.Options{
		Version:          "test",
		BitbucketOptions: []bitbucket.ClientOption{bitbucket.WithHTTPClient(&http.Client{Transport: fixture})},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "pr-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: credentialTransport{}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), result.IsError
}

func TestChecksToolTakesACommitWithoutAPR(t *testing.T) {
	fixture := mergetest.New()
	fixture.Statuses["0011aabb"] = statuses(mergetest.Status("build", "Build", "SUCCESSFUL"))
	text, isError := callMCPTool(t, fixture, "pr_checks", map[string]any{"repo": "acme/widgets", "commit": "0011aabb"})
	if isError || !strings.HasPrefix(text, "# Checks for commit 0011aabb (acme/widgets)") {
		t.Fatalf("commit-only pr_checks: error=%v\n%s", isError, text)
	}

	text, isError = callMCPTool(t, mergetest.New(), "pr_checks", map[string]any{"repo": "acme/widgets"})
	if !isError || !strings.HasPrefix(text, "invalid: give pr (number, URL, or branch) or commit") {
		t.Fatalf("pr_checks without pr or commit: error=%v %s", isError, text)
	}
}
