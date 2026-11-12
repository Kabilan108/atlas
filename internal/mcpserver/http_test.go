package mcpserver_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
)

func serveHandler(t *testing.T, opts mcpserver.Options, defs ...ops.Def) *httptest.Server {
	t.Helper()
	handler, err := mcpserver.HandlerFor(opts, defs)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// A client that sends headers and then stalls mid-body is cut off at the
// body deadline instead of holding a handler.
func TestStalledBodyIsCutOff(t *testing.T) {
	server := serveHandler(t, mcpserver.Options{BodyReadTimeout: 200 * time.Millisecond})
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nContent-Length: 1000\r\n\r\n{\"jsonrpc\":", server.Listener.Addr())

	start := time.Now()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no response to a stalled body: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("cut off after %s, want about the 200ms deadline", elapsed)
	}
}

// The body deadline covers only the body: a tool call that runs past it
// still completes.
func TestBodyDeadlineDoesNotBoundToolCalls(t *testing.T) {
	slow := &ops.Op[noInput, string]{
		Meta: ops.Meta{Name: "slow", Description: "Sleeps.", Surfaces: ops.MCP, ReadOnly: true},
		Run: func(ctx context.Context, _ ops.Env, _ noInput) (string, error) {
			select {
			case <-time.After(600 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	server := serveHandler(t, mcpserver.Options{BodyReadTimeout: 100 * time.Millisecond}, slow)
	text, isError := callText(t, connect(t, server.URL+"/mcp", nil), "slow", nil)
	if isError || text != `"done"` {
		t.Fatalf("isError=%v text=%q", isError, text)
	}
}

func TestLoopbackHostCheck(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		host    string
		want    int
	}{
		{"loopback name", nil, "localhost:8080", http.StatusOK},
		{"loopback IP", nil, "127.0.0.1", http.StatusOK},
		{"proxy name refused by default", nil, "atlas.example.ts.net", http.StatusForbidden},
		{"allowed proxy name", []string{"atlas.example.ts.net"}, "atlas.example.ts.net", http.StatusOK},
		{"allowed name with port, any case", []string{"Atlas.Example.ts.net:443"}, "atlas.example.TS.net:8443", http.StatusOK},
		{"other names still refused", []string{"atlas.example.ts.net"}, "evil.example.com", http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := serveHandler(t, mcpserver.Options{AllowedHosts: tc.allowed}, catalogDefs()...)
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
			req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", "2025-11-25")
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("Host %q: status = %d, want %d", tc.host, resp.StatusCode, tc.want)
			}
		})
	}
}

func TestPanicLogOmitsThePanicValue(t *testing.T) {
	boom := &ops.Op[noInput, string]{
		Meta: ops.Meta{Name: "boom_log", Description: "Panics.", Surfaces: ops.MCP},
		Run: func(context.Context, ops.Env, noInput) (string, error) {
			panic("secret-in-panic-7c1e")
		},
	}
	logs := &syncBuffer{}
	session := connectInMemory(t, mcpserver.Options{Logger: slog.New(slog.NewJSONHandler(logs, nil))}, boom)
	if _, isError := callText(t, session, "boom_log", nil); !isError {
		t.Fatal("panic was not a tool error")
	}
	if strings.Contains(logs.String(), "secret-in-panic-7c1e") {
		t.Fatalf("log contains the panic value:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), `"msg":"tool panicked"`) || !strings.Contains(logs.String(), `"tool":"boom_log"`) {
		t.Fatalf("log lacks the fixed message and tool name:\n%s", logs.String())
	}
}

// A rejected request whose body then stalls gets its error response, and the
// connection closes near the body deadline rather than waiting for net/http
// to drain the rest of the body.
func TestRejectedStalledBodyClosesConnection(t *testing.T) {
	const deadline = 100 * time.Millisecond
	oversized := strings.Repeat("x", 513)
	tests := []struct {
		name    string
		host    string
		headers string
		body    string
		want    int
	}{
		{"oversized Content-Length", "localhost", "Content-Length: 4096\r\n", oversized, http.StatusRequestEntityTooLarge},
		{"oversized chunked", "localhost", "Transfer-Encoding: chunked\r\n", "201\r\n" + oversized + "\r\n", http.StatusRequestEntityTooLarge},
		{"rejected Host", "evil.example", "Content-Length: 1000\r\n", "x", http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := serveHandler(t, mcpserver.Options{MaxBodyBytes: 512, BodyReadTimeout: deadline})
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			start := time.Now()
			fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\n%s\r\n%s", tc.host, tc.headers, tc.body)
			if err := conn.SetReadDeadline(start.Add(10 * deadline)); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatalf("no response within %s: %v", 10*deadline, err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			if _, err := reader.ReadByte(); err != io.EOF {
				t.Fatalf("connection still open %s after the request: %v", time.Since(start), err)
			}
		})
	}
}
