package pr

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
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
	Path        string `json:"path,omitempty" jsonschema:"only this file, or the files under this directory"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"prefix each line with its old and new line numbers, for placing inline comments"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"cap on diff bytes; default 60000, max 8388608"`
}

// DiffResult is a pull request's unified diff.
type DiffResult struct {
	PullRequest int      `json:"pull_request"`
	Files       []string `json:"files"`
	BoundedDiff

	target *Target
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
	limit, err := DiffLimit(in.MaxBytes)
	if err != nil {
		return nil, err
	}
	body, err := client.GetPullRequestDiffLimit(ctx, target.Workspace, target.Repo, target.PR.ID, MaxDiffDownload)
	if err != nil {
		return nil, err
	}
	bounded, files := BoundDiff(body, in.Path, in.LineNumbers, limit)
	return &DiffResult{PullRequest: target.PR.ID, Files: files, BoundedDiff: bounded, target: target}, nil
}

// Markdown is the diff as is, with a note when the path matched nothing or
// the diff was cut short.
func (r *DiffResult) Markdown() (string, error) {
	return r.Render(fileList(r.Files)), nil
}

// BoundedDiff is a unified diff cut down for output: to the files under a
// path, with line numbers, and to a byte cap.
type BoundedDiff struct {
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated,omitempty"`
	// DownloadTruncated is set when the diff exceeded MaxDiffDownload.
	DownloadTruncated bool `json:"download_truncated,omitempty"`

	path      string
	fullBytes int
}

// DiffLimit checks a max_bytes input and returns the cap it means.
func DiffLimit(maxBytes int) (int, error) {
	if maxBytes < 0 || maxBytes > MaxDiffBytes {
		return 0, ops.Errorf(ops.Invalid, "max_bytes must be between 1 and %d (0 means %d)", MaxDiffBytes, DefaultDiffBytes)
	}
	if maxBytes == 0 {
		return DefaultDiffBytes, nil
	}
	return maxBytes, nil
}

// BoundDiff keeps the files of a diff read with MaxDiffDownload that lie
// under path (every file when path is empty), numbers its lines if asked,
// and cuts it at a line boundary to limit bytes. It also returns every file
// the read part of the diff changes.
func BoundDiff(body *bitbucket.LimitedBody, path string, lineNumbers bool, limit int) (BoundedDiff, []string) {
	raw := body.Data
	if body.Truncated {
		raw = raw[:bytes.LastIndexByte(raw, '\n')+1]
	}
	files := DiffFiles(raw)
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path != "" {
		raw = filterDiff(raw, path)
	}
	if lineNumbers {
		raw = numberDiff(raw)
	}
	result := BoundedDiff{DownloadTruncated: body.Truncated, path: path, fullBytes: len(raw)}
	if len(raw) > limit {
		cut := bytes.LastIndexByte(raw[:limit], '\n') + 1
		raw, result.Truncated = raw[:cut], true
	}
	result.Diff = string(raw)
	return result, files
}

// Render is the diff with a note when the path matched nothing or the diff
// was cut short. listing, the changed files as a markdown list, follows
// those notes; an empty listing leaves them without one.
func (d *BoundedDiff) Render(listing string) string {
	changed, read := "", "only its start was read."
	if listing != "" {
		changed, read = " Changed files:", "only its start was read, so the file list may be incomplete."
	}
	if d.path != "" && d.fullBytes == 0 {
		return fmt.Sprintf("No changes under %s.%s\n%s", d.path, changed, listing)
	}
	text := d.Diff
	if d.DownloadTruncated {
		text += fmt.Sprintf("\n[the diff is larger than %d bytes; %s Pass path for one file.]\n", MaxDiffDownload, read)
	}
	if d.Truncated {
		text += fmt.Sprintf("\n[diff truncated: showing %d of %d bytes; pass path for one file or raise max_bytes.%s]\n%s",
			len(d.Diff), d.fullBytes, changed, listing)
	}
	return text
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
