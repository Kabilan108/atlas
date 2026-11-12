package repo

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const (
	defaultFileBytes      = 64 << 10
	defaultBatchFileBytes = 8 << 10
	maxBatchRepos         = 200
	fileReadConcurrency   = 8
)

// FileReadInput is the input to file_read: one file from repo, or the same
// path from each of repos.
type FileReadInput struct {
	ops.RepoRef
	Repos    []string `json:"repos,omitempty" jsonschema:"instead of repo: read path from each (workspace/repo, bare repo, or workspace/* for all), up to 200"`
	Path     string   `json:"path" jsonschema:"file path from the repo root"`
	Ref      string   `json:"ref,omitempty" jsonschema:"branch, tag, or commit; default: each repo's main branch"`
	MaxBytes int      `json:"max_bytes,omitempty" jsonschema:"per-file byte cap; default 65536, or 8192 with repos"`
}

// FileStatus is a file_read entry's outcome.
type FileStatus string

const (
	FileOK     FileStatus = "ok"
	FileFailed FileStatus = "failed"
)

// FileResult is one repository's file, or why it could not be read.
type FileResult struct {
	Index     int        `json:"index"`
	Repo      string     `json:"repo"`
	Status    FileStatus `json:"status"`
	Content   string     `json:"content,omitempty"`
	Bytes     int        `json:"bytes,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	Binary    bool       `json:"binary,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// FileReadResult holds one entry per repository, in input order.
type FileReadResult struct {
	Path    string       `json:"path"`
	Ref     string       `json:"ref,omitempty"`
	Results []FileResult `json:"results"`

	batch    bool
	maxBytes int
}

var FileRead = &ops.Op[FileReadInput, *FileReadResult]{
	Meta: ops.Meta{
		Name:        "file_read",
		Description: "Read a file at a ref from a Bitbucket repo, or the same path across many repos in one call (e.g. version.json audits).",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      readFiles,
	Markdown: (*FileReadResult).Markdown,
}

func readFiles(ctx context.Context, env ops.Env, in FileReadInput) (*FileReadResult, error) {
	path := strings.TrimPrefix(strings.TrimSpace(in.Path), "/")
	if path == "" {
		return nil, ops.Errorf(ops.Invalid, "path is required")
	}
	batch := len(in.Repos) > 0
	if batch && strings.TrimSpace(in.Repo) != "" {
		return nil, ops.Errorf(ops.Invalid, "give repo or repos, not both")
	}
	maxBytes := in.MaxBytes
	switch {
	case maxBytes == 0 && batch:
		maxBytes = defaultBatchFileBytes
	case maxBytes == 0:
		maxBytes = defaultFileBytes
	case maxBytes < -1:
		return nil, ops.Errorf(ops.Invalid, "max_bytes must be positive")
	}
	ref := strings.TrimSpace(in.Ref)
	revision := ref
	if revision == "" {
		revision = "HEAD"
	}

	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	result := &FileReadResult{Path: path, Ref: ref, batch: batch, maxBytes: maxBytes}
	if !batch {
		repo, err := in.Resolve(env)
		if err != nil {
			return nil, err
		}
		entry, err := readOne(ctx, client, repo, revision, path, maxBytes)
		if err != nil {
			return nil, err
		}
		result.Results = []FileResult{entry}
		return result, nil
	}

	repos, err := expandRepos(ctx, env, client, in.Repos)
	if err != nil {
		return nil, err
	}
	result.Results = make([]FileResult, len(repos))
	sem := make(chan struct{}, fileReadConcurrency)
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			entry, err := readOne(ctx, client, repo, revision, path, maxBytes)
			if err != nil {
				entry = FileResult{Repo: repo.String(), Status: FileFailed, Error: entryError(err)}
			}
			entry.Index = i
			result.Results[i] = entry
		}()
	}
	wg.Wait()
	failed := 0
	for _, entry := range result.Results {
		if entry.Status == FileFailed {
			failed++
		}
	}
	return result, ops.PartialOf(failed, len(repos), "files")
}

func readOne(ctx context.Context, client *bitbucket.Client, repo ops.Repository, ref, path string, maxBytes int) (FileResult, error) {
	body, err := client.GetFileContentLimit(ctx, repo.Workspace, repo.Repo, ref, path, int64(maxBytes))
	if err != nil {
		return FileResult{}, err
	}
	entry := FileResult{Repo: repo.String(), Status: FileOK, Bytes: len(body.Data), Truncated: body.Truncated}
	if isBinary(body.Data, body.Truncated) {
		entry.Binary = true
		return entry, nil
	}
	entry.Content = string(body.Data)
	return entry, nil
}

// entryError is err's first line: client errors append a multi-line hint
// that would repeat in every failed entry.
func entryError(err error) string {
	message, _, _ := strings.Cut(err.Error(), "\n")
	return message
}

// isBinary reports whether data holds a NUL byte or invalid UTF-8. A
// truncated body may end mid-rune, which does not count.
func isBinary(data []byte, truncated bool) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	if truncated {
		for i := 0; i < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); i++ {
			data = data[:len(data)-1]
		}
	}
	return !utf8.Valid(data)
}

// expandRepos resolves each repos entry, expanding workspace/* to every
// repository in the workspace, and drops duplicates.
func expandRepos(ctx context.Context, env ops.Env, client *bitbucket.Client, raw []string) ([]ops.Repository, error) {
	var repos []ops.Repository
	seen := map[string]bool{}
	add := func(repo ops.Repository) {
		if key := strings.ToLower(repo.String()); !seen[key] {
			seen[key] = true
			repos = append(repos, repo)
		}
	}
	for _, entry := range raw {
		repo, err := ops.RepoRef{Repo: entry}.ResolveScope(env)
		if err != nil {
			return nil, err
		}
		if !repo.All() {
			add(repo)
			continue
		}
		all, err := client.SearchRepositories(ctx, repo.Workspace, &bitbucket.RepositoryListOptions{Sort: "slug", Limit: maxBatchRepos + 1})
		if err != nil {
			return nil, fmt.Errorf("list repositories in %s: %w", repo.Workspace, err)
		}
		for _, found := range all {
			add(ops.Repository{Workspace: repo.Workspace, Repo: Slug(found)})
		}
	}
	if len(repos) > maxBatchRepos {
		return nil, ops.Errorf(ops.Invalid, "repos names %d or more repositories; read at most %d per call", len(repos), maxBatchRepos)
	}
	return repos, nil
}

// Batch reports whether the input named repos rather than one repo.
func (r *FileReadResult) Batch() bool {
	return r.batch
}

// Markdown renders each file in a fence under its repository, and failures
// as one line each.
func (r *FileReadResult) Markdown() (string, error) {
	var b strings.Builder
	ref := r.Ref
	if ref == "" {
		ref = "main branch"
	}
	if r.batch {
		ok := 0
		for _, entry := range r.Results {
			if entry.Status == FileOK {
				ok++
			}
		}
		fmt.Fprintf(&b, "%s at %s: read from %d of %d repositories\n", r.Path, ref, ok, len(r.Results))
	}
	lang := output.FenceLanguage(r.Path)
	for _, entry := range r.Results {
		if r.batch {
			fmt.Fprintf(&b, "\n## %s\n\n", entry.Repo)
		} else {
			fmt.Fprintf(&b, "%s in %s at %s\n\n", r.Path, entry.Repo, ref)
		}
		switch {
		case entry.Status == FileFailed:
			fmt.Fprintf(&b, "Error: %s\n", entry.Error)
		case entry.Binary:
			fmt.Fprintf(&b, "Binary file, %d bytes%s; not shown.\n", entry.Bytes, truncatedNote(entry))
		default:
			b.WriteString(output.CodeFence(entry.Content, lang))
			if entry.Truncated {
				fmt.Fprintf(&b, "Truncated at %d bytes; raise max_bytes for more.\n", r.maxBytes)
			}
		}
	}
	return b.String(), nil
}

func truncatedNote(entry FileResult) string {
	if entry.Truncated {
		return " or more"
	}
	return ""
}
