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
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// DefaultMaxBodyBytes caps request bodies when Options.MaxBodyBytes is zero.
	DefaultMaxBodyBytes = 1 << 20
	// DefaultBodyReadTimeout bounds reading a request body when
	// Options.BodyReadTimeout is zero.
	DefaultBodyReadTimeout = 30 * time.Second
	// DefaultWorkspace and DefaultJiraCloudID are what `atlas mcp serve`
	// uses when neither a flag nor its environment variable is set.
	DefaultWorkspace   = "moberg-analytics"
	DefaultJiraCloudID = "2a0c794c-e073-4209-873f-ba6c1c4bcd9b"
)

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
	// BodyReadTimeout bounds how long a client may take to send a request
	// body; zero means DefaultBodyReadTimeout. Tool calls themselves are not
	// bounded by it.
	BodyReadTimeout time.Duration
	// AllowedHosts are Host header names accepted on a loopback listener
	// besides localhost and loopback IPs, such as the name a reverse proxy on
	// the same machine forwards. Ports are ignored.
	AllowedHosts []string
	// JiraCloudID selects the Jira site behind the Atlassian API gateway.
	JiraCloudID string
	// BitbucketOptions are applied to every Bitbucket client. Never pass
	// WithCache: the server keeps nothing between calls.
	BitbucketOptions []bitbucket.ClientOption
	// JiraOptions are applied to every Jira client.
	JiraOptions []jira.Option
}

func (o Options) logger() *slog.Logger {
	if o.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Logger
}

// Handler serves catalog.All() at /mcp, with request logging, a Host check
// on loopback listeners, and a size limit and read deadline on bodies.
func Handler(opts Options) (http.Handler, error) {
	return handlerFor(opts, catalog.All())
}

func handlerFor(opts Options, defs []ops.Def) (http.Handler, error) {
	server, err := NewServer(opts, defs)
	if err != nil {
		return nil, err
	}
	maxBody := opts.MaxBodyBytes
	if maxBody == 0 {
		maxBody = DefaultMaxBodyBytes
	}
	bodyTimeout := opts.BodyReadTimeout
	if bodyTimeout == 0 {
		bodyTimeout = DefaultBodyReadTimeout
	}
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		MaxRequestBodyBytes: maxBody,
		// guardHost does the SDK's DNS rebinding check, plus AllowedHosts.
		DisableLocalhostProtection: true,
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", guardHost(opts.AllowedHosts, readBody(maxBody, bodyTimeout, mcpHandler)))
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
		schema, err := ops.InputSchema(def.InputType())
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
				// The panic value may hold request data, so only the tool
				// name and the stack (frames and addresses) are logged.
				opts.logger().Error("tool panicked", "tool", name, "stack", string(debug.Stack()))
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
		res, err := def.Invoke(ctx, newHeaderEnv(header, opts), args)
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

// errorText leads with the error kind ("auth: ...") so agents can branch on
// it, and points API auth failures at the header for that product.
func errorText(err error) string {
	kind := ops.KindOf(err)
	label := kind.String()
	if kind == ops.Unknown {
		label = "error"
	}
	var bitbucketErr *bitbucket.APIError
	if errors.As(err, &bitbucketErr) {
		if hint := authHint(bitbucketErr.StatusCode, HeaderBitbucketToken); hint != "" {
			bitbucketErr.Hint = hint
		}
		return label + ": " + err.Error()
	}
	var jiraErr *jira.APIError
	if errors.As(err, &jiraErr) {
		if hint := authHint(jiraErr.StatusCode, HeaderJiraToken); hint != "" {
			return label + ": " + err.Error() + "\nHint: " + hint
		}
	}
	return label + ": " + err.Error()
}

func authHint(status int, tokenHeader string) string {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("check the %s and %s headers", HeaderEmail, tokenHeader)
	case http.StatusForbidden:
		return fmt.Sprintf("the token in %s may lack a scope this tool needs, or the account lacks access", tokenHeader)
	}
	return ""
}
