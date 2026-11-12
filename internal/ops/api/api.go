// Package api holds the guarded raw GET op, for reads no dedicated op covers
// yet. Every call reports its path template to the context's observer, so
// the paths agents reach for most can become ops.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const defaultMaxBytes = 32 << 10

// HardMaxBytes is the largest max_bytes any caller may ask for.
const HardMaxBytes = 4 << 20

// GetInput is the input to api.
type GetInput struct {
	Path     string `json:"path" jsonschema:"API path with optional query, e.g. /2.0/repositories/acme/app/refs/tags?q=name~\"v1\""`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"response byte cap, default 32768, max 4194304"`
}

// GetResult is a GET response, possibly truncated.
type GetResult struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Body        string `json:"body"`
	Truncated   bool   `json:"truncated,omitempty"`

	maxBytes int
}

var Get = &ops.Op[GetInput, *GetResult]{
	Meta: ops.Meta{
		Name:        "api",
		Description: "GET a Bitbucket REST path (/2.0/...) no other tool covers. Read-only; prefer the dedicated tools.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      get,
	Markdown: (*GetResult).Markdown,
}

// Ops returns the api ops.
func Ops() []ops.Def {
	return []ops.Def{Get}
}

// Route serves the API paths under Prefix. Fetch receives the validated
// path, still carrying Prefix and any query, and must refuse to leave its
// product's API origin. Products add a Route to routes.
type Route struct {
	Prefix string
	Fetch  func(ctx context.Context, env ops.Env, path string, maxBytes int64) (*Response, error)
}

// Response is what a Route fetched.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
	Truncated   bool
}

// routes lists every API the op may call. The Jira slice adds its gateway
// paths (/rest/api/3/..., /rest/agile/...) here.
var routes = []Route{
	bitbucketRoute,
}

var bitbucketRoute = Route{
	Prefix: "/2.0/",
	Fetch: func(ctx context.Context, env ops.Env, path string, maxBytes int64) (*Response, error) {
		client, err := env.Bitbucket(ctx)
		if err != nil {
			return nil, err
		}
		body, err := client.GetAPI(ctx, strings.TrimPrefix(path, "/2.0"), maxBytes)
		if err != nil {
			return nil, err
		}
		return &Response{Status: body.StatusCode, ContentType: body.ContentType, Body: body.Data, Truncated: body.Truncated}, nil
	},
}

func get(ctx context.Context, env ops.Env, in GetInput) (*GetResult, error) {
	path, err := validatePath(in.Path)
	if err != nil {
		return nil, err
	}
	route, ok := routeFor(path)
	if !ok {
		return nil, ops.Errorf(ops.Invalid, "path %q is not an allowed API path; allowed prefixes: %s", in.Path, prefixes())
	}
	maxBytes := in.MaxBytes
	switch {
	case maxBytes == 0:
		maxBytes = defaultMaxBytes
	case maxBytes < 0:
		return nil, ops.Errorf(ops.Invalid, "max_bytes must be positive")
	case maxBytes > HardMaxBytes:
		return nil, ops.Errorf(ops.Invalid, "max_bytes %d is over the maximum of %d", maxBytes, HardMaxBytes)
	}
	observe(ctx, Call{Method: "GET", Template: Template(path)})
	resp, err := route.Fetch(ctx, env, path, int64(maxBytes))
	if err != nil {
		return nil, err
	}
	return &GetResult{
		Method:      "GET",
		Path:        path,
		Status:      resp.Status,
		ContentType: resp.ContentType,
		Body:        string(resp.Body),
		Truncated:   resp.Truncated,
		maxBytes:    maxBytes,
	}, nil
}

// validatePath accepts an absolute path with an optional query and nothing
// that could leave the API: no scheme or host, no backslashes, no dot
// segments (also percent-encoded), no control characters.
func validatePath(raw string) (string, error) {
	path := raw
	invalid := func(reason string) (string, error) {
		return "", ops.Errorf(ops.Invalid, "invalid API path %q: %s", raw, reason)
	}
	switch {
	case strings.TrimSpace(path) == "":
		return invalid("path is required")
	case !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//"):
		return invalid("give a path such as /2.0/user, not a URL")
	case strings.ContainsAny(path, `\#`):
		return invalid("backslashes and fragments are not allowed")
	case strings.ContainsFunc(path, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }):
		return invalid("spaces and control characters are not allowed; percent-encode them")
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return invalid(err.Error())
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" {
		return invalid("absolute URLs are not allowed")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return invalid("dot segments are not allowed")
		}
	}
	return path, nil
}

func routeFor(path string) (Route, bool) {
	for _, route := range routes {
		if strings.HasPrefix(path, route.Prefix) {
			return route, true
		}
	}
	return Route{}, false
}

func prefixes() string {
	names := make([]string, len(routes))
	for i, route := range routes {
		names[i] = route.Prefix
	}
	return strings.Join(names, ", ")
}

// Markdown renders the status line and the body, pretty-printing JSON that
// arrived whole.
func (r *GetResult) Markdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %d\n\n", r.Method, r.Path, r.Status)
	body, lang := r.Body, ""
	if strings.Contains(r.ContentType, "json") {
		lang = "json"
		if !r.Truncated {
			var value any
			if json.Unmarshal([]byte(body), &value) == nil {
				if pretty, err := json.MarshalIndent(value, "", "  "); err == nil {
					body = string(pretty)
				}
			}
		}
	}
	b.WriteString(output.CodeFence(body, lang))
	if r.Truncated {
		fmt.Fprintf(&b, "\nTruncated at %d bytes. Narrow the request (fields=, pagelen=, q=) or use a dedicated tool: pr_view, file_read, commit_list, branch_list, repo_list.\n", r.maxBytes)
	}
	return b.String(), nil
}
