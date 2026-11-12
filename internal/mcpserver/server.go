// Package mcpserver serves the ops catalog as MCP tools over stateless
// Streamable HTTP. Every call brings its own credentials in headers.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultMaxBodyBytes caps request bodies when Options.MaxBodyBytes is zero.
const DefaultMaxBodyBytes = 1 << 20

// Options configures the server.
type Options struct {
	// Version is reported to clients as the server version.
	Version string
	// DefaultWorkspace is used for repo arguments without a workspace.
	DefaultWorkspace string
	// Logger receives one line per HTTP request. Nil discards logs.
	Logger *slog.Logger
	// MaxBodyBytes caps each request body; zero means DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// BitbucketOptions are applied to every Bitbucket client. Never pass
	// WithCache: the server keeps nothing between calls.
	BitbucketOptions []bitbucket.ClientOption
}

func (o Options) logger() *slog.Logger {
	if o.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Logger
}

// Handler serves catalog.All() at /mcp, with request logging and a body
// size limit.
func Handler(opts Options) (http.Handler, error) {
	server, err := NewServer(opts, catalog.All())
	if err != nil {
		return nil, err
	}
	maxBody := opts.MaxBodyBytes
	if maxBody == 0 {
		maxBody = DefaultMaxBodyBytes
	}
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		MaxRequestBodyBytes: maxBody,
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return logRequests(opts.logger(), mux), nil
}

// NewServer registers every op in defs that has the MCP surface.
func NewServer(opts Options, defs []ops.Def) (*mcp.Server, error) {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "atlas", Version: version}, nil)
	server.AddReceivingMiddleware(recordCall)
	for _, def := range defs {
		meta := def.Info()
		if !meta.Surfaces.Has(ops.MCP) {
			continue
		}
		schema, err := jsonschema.ForType(def.InputType(), nil)
		if err != nil {
			return nil, fmt.Errorf("input schema for %s: %w", meta.Name, err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("resolve input schema for %s: %w", meta.Name, err)
		}
		destructive := meta.Destructive
		tool := &mcp.Tool{
			Name:        meta.Name,
			Description: meta.Description,
			InputSchema: schema,
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    meta.ReadOnly,
				DestructiveHint: &destructive,
			},
		}
		server.AddTool(tool, toolHandler(def, resolved, opts))
	}
	return server, nil
}

func toolHandler(def ops.Def, schema *jsonschema.Resolved, opts Options) mcp.ToolHandler {
	name := def.Info().Name
	return func(ctx context.Context, req *mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				opts.logger().Error("tool panicked", "tool", name, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				result, err = errorResult(fmt.Errorf("%s failed with an internal error", name), ""), nil
			}
		}()
		args := req.Params.Arguments
		if err := validateArgs(schema, args); err != nil {
			return errorResult(err, ""), nil
		}
		var header http.Header
		if req.Extra != nil {
			header = req.Extra.Header
		}
		env := &headerEnv{header: header, defaultWorkspace: opts.DefaultWorkspace, clientOptions: opts.BitbucketOptions}
		res, err := def.Invoke(ctx, env, args)
		if err != nil {
			return errorResult(err, res.Text), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: res.Text}}}, nil
	}
}

func validateArgs(schema *jsonschema.Resolved, args json.RawMessage) error {
	var instance any = map[string]any{}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &instance); err != nil {
			return ops.Errorf(ops.Invalid, "invalid arguments: %w", err)
		}
	}
	if err := schema.Validate(instance); err != nil {
		return ops.Errorf(ops.Invalid, "invalid arguments: %w", err)
	}
	return nil
}

// errorResult is a tool error: the model sees it and can retry, unlike a
// JSON-RPC error. partial carries any output produced before the failure.
func errorResult(err error, partial string) *mcp.CallToolResult {
	text := errorText(err)
	if partial != "" {
		text += "\n\n" + partial
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// errorText drops the CLI hints the Bitbucket client attaches, which name
// CLI commands, and points auth failures at the credential headers.
func errorText(err error) string {
	text := err.Error()
	var apiErr *bitbucket.APIError
	if errors.As(err, &apiErr) && apiErr.Hint != "" {
		text = strings.Replace(text, apiErr.Error(), apiErr.Resource+": "+apiErr.Message, 1)
	}
	if ops.KindOf(err) == ops.Auth && apiErr != nil {
		text += fmt.Sprintf(" (check the %s and %s headers)", HeaderEmail, HeaderBitbucketToken)
	}
	return text
}
