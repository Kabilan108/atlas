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

// MaxDiffBytes is the largest output cap a caller may ask for, and
// MaxDiffDownload the most of a diff pr_diff ever reads. The download cap is
// larger because a path filter and the changed-file list need the whole diff.
const (
	MaxDiffBytes    = 8 << 20
	MaxDiffDownload = 16 << 20
)

// DiffInput is the input to pr_diff.
type DiffInput struct {
	Ref
	Path     string `json:"path,omitempty" jsonschema:"only this file, or the files under this directory"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"cap on diff bytes; default 60000, max 8388608"`
}

// DiffResult is a pull request's unified diff.
type DiffResult struct {
	PullRequest int      `json:"pull_request"`
	Files       []string `json:"files"`
	Diff        string   `json:"diff"`
	Truncated   bool     `json:"truncated,omitempty"`
	// DownloadTruncated is set when the diff exceeded MaxDiffDownload.
	DownloadTruncated bool `json:"download_truncated,omitempty"`

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
	if in.MaxBytes < 0 || in.MaxBytes > MaxDiffBytes {
		return nil, ops.Errorf(ops.Invalid, "max_bytes must be between 1 and %d (0 means %d)", MaxDiffBytes, DefaultDiffBytes)
	}
	body, err := client.GetPullRequestDiffLimit(ctx, target.Workspace, target.Repo, target.PR.ID, MaxDiffDownload)
	if err != nil {
		return nil, err
	}
	raw := body.Data
	if body.Truncated {
		raw = raw[:bytes.LastIndexByte(raw, '\n')+1]
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	result := &DiffResult{PullRequest: target.PR.ID, Files: DiffFiles(raw), target: target, path: path, DownloadTruncated: body.Truncated}
	if path != "" {
		raw = filterDiff(raw, path)
	}
	result.fullBytes = len(raw)
	limit := in.MaxBytes
	if limit == 0 {
		limit = DefaultDiffBytes
	}
	if len(raw) > limit {
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
	if r.DownloadTruncated {
		note := fmt.Sprintf("\n[the diff is larger than %d bytes; only its start was read, so the file list may be incomplete. Pass path for one file.]\n", MaxDiffDownload)
		if !r.Truncated {
			return r.Diff + note, nil
		}
		r.Diff += note
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

// DiffFiles lists the files a unified diff changes, in order, each once:
// the new name, or the old one for a deleted file.
func DiffFiles(diff []byte) []string {
	seen := make(map[string]bool)
	var names []string
	for _, section := range diffSections(diff) {
		name := section.newName
		if name == "" {
			name = section.oldName
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// filterDiff keeps the file sections of diff whose old or new name is path,
// or lies under path taken as a directory. Names compare whole path
// components, so "doc" matches neither "docs/a.md" nor "b/doc".
func filterDiff(diff []byte, path string) []byte {
	var out bytes.Buffer
	for _, section := range diffSections(diff) {
		if pathMatches(section.oldName, path) || pathMatches(section.newName, path) {
			out.Write(section.text)
		}
	}
	return out.Bytes()
}

func pathMatches(name, path string) bool {
	return name != "" && (name == path || strings.HasPrefix(name, path+"/"))
}
