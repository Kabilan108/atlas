package pr

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/ops"
)

// DefaultDiffBytes caps pr_diff output when DiffInput.MaxBytes is zero.
const DefaultDiffBytes = 60000

// DiffInput is the input to pr_diff.
type DiffInput struct {
	Ref
	Path     string `json:"path,omitempty" jsonschema:"only this file, or the files under this directory"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"cap on diff bytes; default 60000"`
}

// DiffResult is a pull request's unified diff.
type DiffResult struct {
	PullRequest int      `json:"pull_request"`
	Files       []string `json:"files"`
	Diff        string   `json:"diff"`
	Truncated   bool     `json:"truncated,omitempty"`

	target    *Target
	path      string
	fullBytes int
}

// Target is the pull request the diff belongs to.
func (r *DiffResult) Target() *Target {
	return r.target
}

var Diff = &ops.Op[DiffInput, *DiffResult]{
	Meta: ops.Meta{
		Name:        "pr_diff",
		Description: "Get a Bitbucket pull request's unified diff, optionally for one path. The result lists every changed file.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      diff,
	Markdown: (*DiffResult).Markdown,
}

func diff(ctx context.Context, env ops.Env, in DiffInput) (*DiffResult, error) {
	target, err := in.Resolve(ctx, env)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := client.GetPullRequestDiff(ctx, target.Workspace, target.Repo, target.PR.ID)
	if err != nil {
		return nil, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	result := &DiffResult{PullRequest: target.PR.ID, Files: DiffFiles(raw), target: target, path: path}
	if path != "" {
		raw = filterDiff(raw, path)
	}
	result.fullBytes = len(raw)
	limit := in.MaxBytes
	if limit == 0 {
		limit = DefaultDiffBytes
	}
	if limit > 0 && len(raw) > limit {
		cut := bytes.LastIndexByte(raw[:limit], '\n') + 1
		raw, result.Truncated = raw[:cut], true
	}
	result.Diff = string(raw)
	return result, nil
}

// Markdown is the diff as is, with a note when the path matched nothing or
// the diff was cut short.
func (r *DiffResult) Markdown() (string, error) {
	if r.path != "" && r.fullBytes == 0 {
		return fmt.Sprintf("No changes under %s. Changed files:\n%s", r.path, fileList(r.Files)), nil
	}
	if !r.Truncated {
		return r.Diff, nil
	}
	return fmt.Sprintf("%s\n[diff truncated: showing %d of %d bytes; pass path for one file or raise max_bytes. Changed files:]\n%s",
		r.Diff, len(r.Diff), r.fullBytes, fileList(r.Files)), nil
}

func fileList(files []string) string {
	var b strings.Builder
	for _, file := range files {
		fmt.Fprintf(&b, "- %s\n", file)
	}
	return b.String()
}

// DiffFiles lists the files a unified diff changes, in order, each once.
func DiffFiles(diff []byte) []string {
	seen := make(map[string]bool)
	var names []string
	for _, line := range strings.Split(string(diff), "\n") {
		name, ok := diffHeaderName(line)
		if ok && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func diffHeaderName(line string) (string, bool) {
	if !strings.HasPrefix(line, "diff --git ") {
		return "", false
	}
	parts := strings.Split(line, " ")
	if len(parts) < 4 {
		return "", false
	}
	name := strings.TrimPrefix(parts[3], "b/")
	if name == "/dev/null" {
		name = strings.TrimPrefix(parts[2], "a/")
	}
	return name, true
}

// filterDiff keeps the file sections of diff whose old or new name is path
// or lies under it.
func filterDiff(diff []byte, path string) []byte {
	var out bytes.Buffer
	keep := false
	for _, line := range bytes.SplitAfter(diff, []byte("\n")) {
		if header := string(bytes.TrimRight(line, "\n")); strings.HasPrefix(header, "diff --git ") {
			keep = false
			parts := strings.Split(header, " ")
			for _, part := range parts[2:] {
				name := strings.TrimPrefix(strings.TrimPrefix(part, "a/"), "b/")
				if name == path || strings.HasPrefix(name, path+"/") {
					keep = true
				}
			}
		}
		if keep {
			out.Write(line)
		}
	}
	return out.Bytes()
}
