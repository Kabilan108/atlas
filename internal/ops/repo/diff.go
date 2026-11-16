package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/output"
)

// maxDiffFiles bounds the per-file summary, which Bitbucket sends 500 to a
// page.
const maxDiffFiles = 500

// DiffInput is the input to repo_diff.
type DiffInput struct {
	ops.RepoRef
	Base        string `json:"base" jsonschema:"branch, tag, or commit to compare against, e.g. main"`
	Head        string `json:"head" jsonschema:"branch, tag, or commit with the changes"`
	Path        string `json:"path,omitempty" jsonschema:"diff only this file or directory"`
	FilesOnly   bool   `json:"files_only,omitempty" jsonschema:"only the per-file summary, no diff"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"prefix diff lines with their old and new line numbers"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"cap on diff bytes; default 60000, max 8388608"`
}

// FileChange is one changed file. OldPath is set for a rename.
type FileChange struct {
	Status       string `json:"status"`
	Path         string `json:"path"`
	OldPath      string `json:"old_path,omitempty"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
}

// DiffResult is what head changes relative to its merge base with base:
// every changed file, then the diff unless only files were asked for.
type DiffResult struct {
	Repo      string       `json:"repo"`
	Base      string       `json:"base"`
	Head      string       `json:"head"`
	Files     []FileChange `json:"files"`
	MoreFiles bool         `json:"more_files,omitempty"`
	pr.BoundedDiff
}

var Diff = &ops.Op[DiffInput, *DiffResult]{
	Meta: ops.Meta{
		Name: "repo_diff",
		Description: "Diff two Bitbucket refs without a pull request: a per-file summary, then the unified diff. " +
			"Like a PR, it shows head's changes since its merge base with base.",
		Surfaces: ops.Both,
		ReadOnly: true,
	},
	Run:      diff,
	Markdown: (*DiffResult).Markdown,
}

func diff(ctx context.Context, env ops.Env, in DiffInput) (*DiffResult, error) {
	repo, err := in.Resolve(env)
	if err != nil {
		return nil, err
	}
	base, err := compareRef("base", in.Base)
	if err != nil {
		return nil, err
	}
	head, err := compareRef("head", in.Head)
	if err != nil {
		return nil, err
	}
	if in.FilesOnly && strings.TrimSpace(in.Path) != "" {
		return nil, ops.Errorf(ops.Invalid, "path narrows the diff, which files_only leaves out; drop one")
	}
	limit, err := pr.DiffLimit(in.MaxBytes)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	spec, err := client.CompareSpec(ctx, repo.Workspace, repo.Repo, base, head)
	if err != nil {
		return nil, err
	}
	stats, scan, err := client.ListCompareDiffStat(ctx, repo.Workspace, repo.Repo, spec, maxDiffFiles)
	if err != nil {
		return nil, err
	}
	result := &DiffResult{Repo: repo.String(), Base: base, Head: head, Files: fileChanges(stats), MoreFiles: scan.More}
	if in.FilesOnly || len(stats) == 0 {
		return result, nil
	}
	body, err := client.GetCompareDiffLimit(ctx, repo.Workspace, repo.Repo, spec, pr.MaxDiffDownload)
	if err != nil {
		return nil, err
	}
	result.BoundedDiff, _ = pr.BoundDiff(body, in.Path, in.LineNumbers, limit)
	return result, nil
}

// compareRef trims a ref and rejects one that cannot sit in a Bitbucket
// head..base spec.
func compareRef(name, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return "", ops.Errorf(ops.Invalid, "%s is required: give a branch, tag, or commit", name)
	case strings.Contains(ref, ".."):
		return "", ops.Errorf(ops.Invalid, "%s %q cannot contain \"..\"; give base and head separately", name, ref)
	}
	return ref, nil
}

func fileChanges(stats []bitbucket.DiffStat) []FileChange {
	changes := make([]FileChange, 0, len(stats))
	for _, stat := range stats {
		change := FileChange{Status: stat.Status, LinesAdded: stat.LinesAdded, LinesRemoved: stat.LinesRemoved}
		if stat.New != nil {
			change.Path = stat.New.Path
		}
		if stat.Old != nil {
			if change.Path == "" {
				change.Path = stat.Old.Path
			} else if stat.Old.Path != change.Path {
				change.OldPath = stat.Old.Path
			}
		}
		changes = append(changes, change)
	}
	return changes
}

// Markdown is the file summary as a table, then the diff with pr_diff's
// notes for a path that matched nothing or a diff cut short.
func (r *DiffResult) Markdown() (string, error) {
	scope := r.Repo + " " + r.Base + "..." + r.Head
	if len(r.Files) == 0 {
		return "No changes in " + scope + ": " + r.Head + " adds nothing since its merge base with " + r.Base + ".\n", nil
	}
	added, removed := 0, 0
	rows := make([][]string, 0, len(r.Files))
	for _, file := range r.Files {
		added, removed = added+file.LinesAdded, removed+file.LinesRemoved
		path := file.Path
		if file.OldPath != "" {
			path = file.OldPath + " → " + file.Path
		}
		rows = append(rows, []string{file.Status, path, "+" + strconv.Itoa(file.LinesAdded), "-" + strconv.Itoa(file.LinesRemoved)})
	}
	shown := ""
	if r.MoreFiles {
		shown = fmt.Sprintf(" (the first %d)", len(r.Files))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Changes in %s: %d files%s, +%d -%d\n\n", scope, len(r.Files), shown, added, removed)
	b.WriteString(output.MarkdownTable([]string{"Status", "File", "Added", "Removed"}, rows))
	if diff := r.Render(""); diff != "" {
		b.WriteString("\n" + diff)
	}
	return b.String(), nil
}
