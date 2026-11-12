// Package snippet holds the Bitbucket snippet operations.
package snippet

import (
	"net/url"
	"strings"

	"github.com/kabilan108/atlas/internal/ops"
)

// Ref is a validated snippet: its workspace and ID.
type Ref struct {
	Workspace string
	ID        string
}

// Target is the input that names one snippet. Every op on an existing
// snippet embeds it.
type Target struct {
	Snippet   string `json:"snippet" jsonschema:"snippet ID or Bitbucket snippet URL"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace of a snippet ID; default: the default workspace"`
}

// Resolve parses t.Snippet. A URL names its own workspace; an ID uses
// t.Workspace or the default workspace.
func (t Target) Resolve(env ops.Env) (Ref, error) {
	fallback := strings.TrimSpace(t.Workspace)
	if fallback == "" {
		fallback = env.DefaultWorkspace()
	}
	return ParseRef(t.Snippet, fallback)
}

// ParseRef accepts a snippet ID, which lives in fallbackWorkspace, or a
// bitbucket.org snippet URL (web, clone, or API form), which names its own
// workspace.
func ParseRef(raw, fallbackWorkspace string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, ops.Errorf(ops.Invalid, "snippet ID or URL is required")
	}

	var ref Ref
	if IsURL(raw) {
		parsed, _ := url.Parse(raw)
		host := parsed.Hostname()
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		var err error
		switch {
		case host == "bitbucket.org" && len(parts) >= 3 && parts[0] == "snippets":
			ref, err = refFromURLParts(parts[1], parts[2])
		case host == "bitbucket.org" && len(parts) >= 4 && parts[1] == "workspace" && parts[2] == "snippets":
			ref, err = refFromURLParts(parts[0], parts[3])
		case isBitbucketHost(host) && len(parts) >= 4 && parts[0] == "2.0" && parts[1] == "snippets":
			ref, err = refFromURLParts(parts[2], parts[3])
		default:
			return Ref{}, ops.Errorf(ops.Invalid, "unsupported snippet URL: %s", raw)
		}
		if err != nil {
			return Ref{}, err
		}
	} else {
		if fallbackWorkspace == "" {
			return Ref{}, ops.Errorf(ops.Invalid, "workspace is required for snippet ID %q: give workspace or a snippet URL", raw)
		}
		ref = Ref{Workspace: fallbackWorkspace, ID: raw}
	}
	if err := ops.ValidateSlug("workspace", ref.Workspace); err != nil {
		return Ref{}, err
	}
	if err := ops.ValidateSlug("snippet ID", ref.ID); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// IsURL reports whether raw has a host, so it names its own workspace.
func IsURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Host != ""
}

func isBitbucketHost(host string) bool {
	return host == "bitbucket.org" || strings.HasSuffix(host, ".bitbucket.org")
}

func refFromURLParts(workspacePart, idPart string) (Ref, error) {
	workspace, err := url.PathUnescape(workspacePart)
	if err != nil {
		return Ref{}, ops.Errorf(ops.Invalid, "invalid snippet workspace in URL: %w", err)
	}
	id, err := url.PathUnescape(strings.TrimSuffix(idPart, ".git"))
	if err != nil {
		return Ref{}, ops.Errorf(ops.Invalid, "invalid snippet ID in URL: %w", err)
	}
	if workspace == "" || id == "" {
		return Ref{}, ops.Errorf(ops.Invalid, "snippet URL must include workspace and ID")
	}
	return Ref{Workspace: workspace, ID: id}, nil
}

// validateFilename rejects names Bitbucket's file paths cannot carry.
func validateFilename(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return ops.Errorf(ops.Invalid, "snippet filename is empty")
	case strings.HasPrefix(name, "/"):
		return ops.Errorf(ops.Invalid, "snippet filename %q cannot be absolute", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ops.Errorf(ops.Invalid, "snippet filename %q has an empty or dot segment", name)
		}
	}
	return nil
}
