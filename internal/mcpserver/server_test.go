package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testEmail = "agent@example.org"
	testToken = "fake-bitbucket-token-7f3a9c"
)

// syncBuffer is a log sink the server's goroutines can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// headerTransport adds fixed headers to every request, as an MCP client
// configured with Atlas credentials would.
type headerTransport struct {
	header http.Header
	base   http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for name, values := range t.header {
		r.Header[name] = values
	}
	return t.base.RoundTrip(r)
}

type testServer struct {
	url     string
	fixture *prtest.Fixture
	logs    *syncBuffer
	// close stops the server and waits for in-flight handlers, which log
	// after the client already has its response.
	close func()
}

func startServer(t *testing.T, opts mcpserver.Options) testServer {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fixture := prtest.New()
	logs := &syncBuffer{}
	opts.Version = "test"
	opts.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	opts.BitbucketOptions = []bitbucket.ClientOption{bitbucket.WithHTTPClient(&http.Client{Transport: fixture})}
	handler, err := mcpserver.Handler(opts)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return testServer{url: server.URL + "/mcp", fixture: fixture, logs: logs, close: server.Close}
}

func connect(t *testing.T, endpoint string, header http.Header) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "atlas-test", Version: "test"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: headerTransport{header: header, base: &http.Transport{}}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func credentials() http.Header {
	return http.Header{
		mcpserver.HeaderEmail:          {testEmail},
		mcpserver.HeaderBitbucketToken: {testToken},
	}
}

func callText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
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

func TestToolsListIsLean(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	session := connect(t, srv.url, nil)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d bytes: %s", tool.Name, len(data), data)
		if len(data) >= 1024 {
			t.Errorf("%s is %d bytes in tools/list, want under 1024", tool.Name, len(data))
		}
		if tool.Name == "pr_view" {
			found = tool
		}
	}
	if found == nil {
		t.Fatal("tools/list lacks pr_view")
	}
	if found.Annotations == nil || !found.Annotations.ReadOnlyHint {
		t.Fatalf("pr_view annotations = %+v, want read-only", found.Annotations)
	}
	schema, err := json.Marshal(found.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"repo", "pr", "comments", "include_resolved"} {
		if _, ok := parsed.Properties[name]; !ok {
			t.Errorf("schema lacks %q: %s", name, schema)
		}
	}
	if len(parsed.Properties) != 4 {
		t.Errorf("schema exposes CLI-only fields: %s", schema)
	}
	if len(parsed.Required) != 1 || parsed.Required[0] != "pr" {
		t.Errorf("required = %v, want [pr]", parsed.Required)
	}
}

func TestMissingHeadersAreToolErrors(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	tests := []struct {
		name    string
		header  http.Header
		missing []string
	}{
		{"none", nil, []string{mcpserver.HeaderEmail, mcpserver.HeaderBitbucketToken}},
		{"token only", http.Header{mcpserver.HeaderBitbucketToken: {testToken}}, []string{mcpserver.HeaderEmail}},
		{"email only", http.Header{mcpserver.HeaderEmail: {testEmail}}, []string{mcpserver.HeaderBitbucketToken}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := connect(t, srv.url, tc.header)
			text, isError := callText(t, session, "pr_view", map[string]any{"repo": "acme/widgets", "pr": "42"})
			if !isError {
				t.Fatalf("missing headers succeeded: %s", text)
			}
			for _, header := range tc.missing {
				if !strings.Contains(text, header) {
					t.Fatalf("error %q does not name %s", text, header)
				}
			}
		})
	}
	if n := len(srv.fixture.Requests()); n != 0 {
		t.Fatalf("calls without credentials reached Bitbucket %d times", n)
	}
}

func TestToolCallUsesHeaderCredentials(t *testing.T) {
	srv := startServer(t, mcpserver.Options{DefaultWorkspace: prtest.Workspace})
	session := connect(t, srv.url, credentials())
	text, isError := callText(t, session, "pr_view", map[string]any{"repo": prtest.Repo, "pr": "42", "comments": true})
	if isError {
		t.Fatal(text)
	}
	if want := prtest.Golden("view_comments.md"); text != want {
		t.Fatalf("pr_view text differs from view_comments.md\n--- got ---\n%s", text)
	}
	requests := srv.fixture.Requests()
	if len(requests) == 0 {
		t.Fatal("no Bitbucket requests")
	}
	for _, req := range requests {
		if req.Username != testEmail || req.Password != testToken {
			t.Fatalf("%s sent credentials %q/%q", req.Path, req.Username, req.Password)
		}
	}
}

// The confirmed P2: a repo with a query and fragment changed the endpoint.
func TestRepoComponentsCannotRedirectTheEndpoint(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	session := connect(t, srv.url, credentials())
	text, isError := callText(t, session, "pr_view", map[string]any{"repo": "acme/widgets?fields=description#", "pr": "42"})
	if !isError || !strings.Contains(text, "must not contain") {
		t.Fatalf("isError=%v text=%q", isError, text)
	}
	if n := len(srv.fixture.Requests()); n != 0 {
		t.Fatalf("invalid repo reached Bitbucket %d times: %v", n, srv.fixture.Requests())
	}
}

func TestInvalidArgumentsAreToolErrors(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	session := connect(t, srv.url, credentials())
	for _, args := range []map[string]any{
		{"repo": "acme/widgets"},
		{"repo": "acme/widgets", "pr": 42},
		{"repo": "acme/widgets", "pr": "42", "current_branch": "main"},
	} {
		text, isError := callText(t, session, "pr_view", args)
		if !isError || !strings.Contains(text, "invalid arguments") {
			t.Fatalf("args %v: isError=%v text=%q", args, isError, text)
		}
	}
}

func TestLogsRecordCallsButNeverCredentials(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	session := connect(t, srv.url, credentials())
	if text, isError := callText(t, session, "pr_view", map[string]any{"repo": "acme/widgets", "pr": "42"}); isError {
		t.Fatal(text)
	}
	if text, isError := callText(t, session, "pr_view", map[string]any{"repo": "acme/widgets", "pr": "7"}); !isError || !strings.HasPrefix(text, "not_found: ") {
		t.Fatalf("missing PR: isError=%v text=%q", isError, text)
	}
	anonymous := connect(t, srv.url, http.Header{mcpserver.HeaderBitbucketToken: {testToken}})
	if _, isError := callText(t, anonymous, "pr_view", map[string]any{"repo": "acme/widgets", "pr": "42"}); !isError {
		t.Fatal("call without email succeeded")
	}

	srv.close()
	logs := srv.logs.String()
	for _, secret := range []string{testToken, testEmail} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs contain %q:\n%s", secret, logs)
		}
	}
	var toolLines, toolErrors int
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		for _, field := range []string{"method", "path", "status", "duration_ms"} {
			if _, ok := entry[field]; !ok {
				t.Fatalf("log line lacks %s: %q", field, line)
			}
		}
		if entry["tool"] == "pr_view" {
			toolLines++
			if entry["tool_error"] == true {
				toolErrors++
			}
		}
	}
	if toolLines != 3 || toolErrors != 2 {
		t.Fatalf("logged %d pr_view calls with %d errors, want 3 and 2:\n%s", toolLines, toolErrors, logs)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	srv := startServer(t, mcpserver.Options{MaxBodyBytes: 512})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pr_view","arguments":{"pr":"` + strings.Repeat("x", 1024) + `"}}}`
	req, err := http.NewRequest(http.MethodPost, srv.url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := (&http.Client{Transport: &http.Transport{}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

type panicInput struct{}

func TestToolPanicIsRecovered(t *testing.T) {
	boom := &ops.Op[panicInput, string]{
		Meta: ops.Meta{Name: "boom", Description: "Panics.", Surfaces: ops.MCP, ReadOnly: true},
		Run: func(context.Context, ops.Env, panicInput) (string, error) {
			panic("kaboom: internal detail")
		},
	}
	logs := &syncBuffer{}
	session := connectInMemory(t, mcpserver.Options{Logger: slog.New(slog.NewJSONHandler(logs, nil))}, boom)

	for range 2 {
		text, isError := callText(t, session, "boom", nil)
		if !isError || !strings.Contains(text, "internal error") {
			t.Fatalf("isError=%v text=%q", isError, text)
		}
		if strings.Contains(text, "internal detail") {
			t.Fatalf("panic value leaked to the client: %q", text)
		}
	}
	if !strings.Contains(logs.String(), "tool panicked") {
		t.Fatalf("panic not logged:\n%s", logs.String())
	}
}
