package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callRecord collects what the MCP layer learns about a request, for the
// request log line. The SDK may run handlers on another goroutine.
type callRecord struct {
	mu        sync.Mutex
	rpc       string
	tool      string
	toolError bool
}

func (c *callRecord) set(update func(*callRecord)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	update(c)
}

func (c *callRecord) attrs() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var attrs []any
	if c.rpc != "" {
		attrs = append(attrs, "rpc", c.rpc)
	}
	if c.tool != "" {
		attrs = append(attrs, "tool", c.tool, "tool_error", c.toolError)
	}
	return attrs
}

type callRecordKey struct{}

// logRequests logs one line per request: method, path, JSON-RPC method,
// tool, status, and duration. It never logs headers, which carry
// credentials, or bodies.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		record := &callRecord{}
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), callRecordKey{}, record)))
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.statusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
		}
		logger.Info("request", append(attrs, record.attrs()...)...)
	})
}

// recordCall copies the JSON-RPC method, tool name, and tool outcome into
// the request's callRecord.
func recordCall(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		record, _ := ctx.Value(callRecordKey{}).(*callRecord)
		if record == nil {
			return next(ctx, method, req)
		}
		record.set(func(c *callRecord) {
			c.rpc = method
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
				c.tool = call.Params.Name
			}
		})
		result, err := next(ctx, method, req)
		if toolResult, ok := result.(*mcp.CallToolResult); ok {
			record.set(func(c *callRecord) { c.toolError = toolResult.IsError })
		}
		return result, err
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
