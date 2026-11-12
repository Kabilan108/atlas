package repo

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const (
	defaultFileBytes      = 64 << 10
	defaultBatchFileBytes = 8 << 10
	// HardMaxFileBytes is the largest max_bytes for one file.
	HardMaxFileBytes = 4 << 20
	// HardMaxBatchFileBytes is the largest max_bytes with repos.
	HardMaxBatchFileBytes = 1 << 20
	// batchBudgetBytes bounds what one batch reads across all repositories:
	// each file's cap shrinks to an equal share when max_bytes would exceed it.
	batchBudgetBytes    = 8 << 20
	maxBatchRepos       = 200
	fileReadConcurrency = 8
)

// FileReadInput is the input to file_read: one file from repo, or the same
// path from each of repos.
type FileReadInput struct {
	ops.RepoRef
	Repos    []string `json:"repos,omitempty" jsonschema:"instead of repo: read path from each (workspace/repo, bare repo, or workspace/* for all), up to 200"`
	Path     string   `json:"path" jsonschema:"file path from the repo root"`
	Ref      string   `json:"ref,omitempty" jsonschema:"branch, tag, or commit; default: each repo's main branch"`
	MaxBytes int      `json:"max_bytes,omitempty" jsonschema:"per-file byte cap; default 65536, max 4194304; with repos default 8192, max 1048576"`
}

// FileStatus is a file_read entry's outcome.
type FileStatus string

const (
	FileOK FileStatus = "ok"
	// FileNotFound marks a repository without the file, which is not a
	// failure: audits expect many.
	FileNotFound FileStatus = "not_found"
	FileFailed   FileStatus = "failed"
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
	// budgetShare is true when the batch budget lowered maxBytes below
	// what the caller asked for.
	budgetShare bool
}

var FileRead = &ops.Op[FileReadInput, *FileReadResult]{
	Meta: ops.Meta{
		Name:        "file_read",
		Description: "Read a file at a ref from a Bitbucket repo, or the same path across many repos in one call (e.g. version.json audits). Repos without the file get status not_found, which is not an error.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      readFiles,
	Markdown: (*FileReadResult).Markdown,
}

func readFiles(ctx context.Context, env ops.Env, in FileReadInput) (*FileReadResult, error) {
	path := in.Path
	if err := validateFilePath(path); err != nil {
		return nil, err
	}
	batch := len(in.Repos) > 0
	if batch && strings.TrimSpace(in.Repo) != "" {
		return nil, ops.Errorf(ops.Invalid, "give repo or repos, not both")
	}
	maxBytes, err := fileByteCap(in.MaxBytes, batch)
	if err != nil {
		return nil, err
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
	if len(repos) > 0 && maxBytes > batchBudgetBytes/len(repos) {
		maxBytes = batchBudgetBytes / len(repos)
		result.maxBytes, result.budgetShare = maxBytes, true
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
				status := FileFailed
				if ops.KindOf(err) == ops.NotFound {
					status = FileNotFound
				}
				entry = FileResult{Repo: repo.String(), Status: status, Error: entryError(err)}
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

// fileByteCap applies max_bytes' default and rejects values outside
// (0, hard maximum].
func fileByteCap(maxBytes int, batch bool) (int, error) {
	def, hardMax := defaultFileBytes, HardMaxFileBytes
	if batch {
		def, hardMax = defaultBatchFileBytes, HardMaxBatchFileBytes
	}
	switch {
	case maxBytes == 0:
		return def, nil
	case maxBytes < 0:
		return 0, ops.Errorf(ops.Invalid, "max_bytes must be positive")
	case maxBytes > hardMax:
		return 0, ops.Errorf(ops.Invalid, "max_bytes %d is over the maximum of %d", maxBytes, hardMax)
	}
	return maxBytes, nil
}

// validateFilePath accepts a path from the repo root exactly as given, since
// file names may start or end with spaces: no leading slash, no empty, ".",
// or ".." components, and no control characters.
func validateFilePath(path string) error {
	invalid := func(reason string) error {
		return ops.Errorf(ops.Invalid, "invalid file path %q: %s", path, reason)
	}
	switch {
	case path == "":
		return ops.Errorf(ops.Invalid, "path is required")
	case strings.HasPrefix(path, "/"):
		return invalid("give it from the repo root, without a leading /")
	case strings.ContainsFunc(path, unicode.IsControl):
		return invalid("control characters are not allowed")
	}
	for _, component := range strings.Split(path, "/") {
		switch component {
		case "":
			return invalid("empty components (// or a trailing /) are not allowed")
		case ".", "..":
			return invalid("dot components are not allowed")
		}
	}
	return nil
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
// truncated body may end in the valid first bytes of a rune the cap cut
// off; any other invalid byte means binary.
func isBinary(data []byte, truncated bool) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	if truncated {
		data = data[:len(data)-len(incompleteRuneSuffix(data))]
	}
	return !utf8.Valid(data)
}

// incompleteRuneSuffix is data's trailing rune when it is a valid UTF-8
// prefix the data ends before completing, or nil.
func incompleteRuneSuffix(data []byte) []byte {
	for i := len(data) - 1; i >= 0 && i >= len(data)-(utf8.UTFMax-1); i-- {
		if utf8.RuneStart(data[i]) {
			if tail := data[i:]; !utf8.FullRune(tail) {
				return tail
			}
			return nil
		}
	}
	return nil
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
		ok, notFound := 0, 0
		for _, entry := range r.Results {
			switch entry.Status {
			case FileOK:
				ok++
			case FileNotFound:
				notFound++
			}
		}
		fmt.Fprintf(&b, "%s at %s: read from %d of %d repositories", r.Path, ref, ok, len(r.Results))
		if notFound > 0 {
			fmt.Fprintf(&b, "; not found in %d", notFound)
		}
		b.WriteString("\n")
	}
	lang := output.FenceLanguage(r.Path)
	for _, entry := range r.Results {
		if r.batch {
			fmt.Fprintf(&b, "\n## %s\n\n", entry.Repo)
		} else {
			fmt.Fprintf(&b, "%s in %s at %s\n\n", r.Path, entry.Repo, ref)
		}
		switch {
		case entry.Status == FileNotFound:
			fmt.Fprintf(&b, "Not found: %s\n", entry.Error)
		case entry.Status == FileFailed:
			fmt.Fprintf(&b, "Error: %s\n", entry.Error)
		case entry.Binary:
			fmt.Fprintf(&b, "Binary file, %d bytes%s; not shown.\n", entry.Bytes, truncatedNote(entry))
		default:
			b.WriteString(output.CodeFence(entry.Content, lang))
			if entry.Truncated {
				b.WriteString(r.truncationNote())
			}
		}
	}
	return b.String(), nil
}

func (r *FileReadResult) truncationNote() string {
	if r.budgetShare {
		return fmt.Sprintf("Truncated at %d bytes, this repository's share of the %d-byte batch budget; read fewer repositories for more.\n", r.maxBytes, batchBudgetBytes)
	}
	return fmt.Sprintf("Truncated at %d bytes; raise max_bytes for more.\n", r.maxBytes)
}

func truncatedNote(entry FileResult) string {
	if entry.Truncated {
		return " or more"
	}
	return ""
}
