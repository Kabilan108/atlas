package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// In a linked worktree .git is a file pointing into the main repository's
// .git/worktrees, and origin is only in the common config.
func TestInferRepositoryInWorktree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "init", "-q")
	runGit(t, main, "remote", "add", "origin", "git@bitbucket.org:acme/widgets.git")
	runGit(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	worktree := filepath.Join(root, "linked")
	runGit(t, main, "worktree", "add", "-q", "-b", "feature", worktree)
	if info, err := os.Stat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
		t.Fatalf("worktree .git = %v, %v; want a file", info, err)
	}

	for _, dir := range []string{main, worktree, filepath.Join(worktree, "nested")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		workspace, repo, err := InferRepository()
		if err != nil || workspace != "acme" || repo != "widgets" {
			t.Fatalf("InferRepository() in %s = %q, %q, %v", dir, workspace, repo, err)
		}
	}
}

func TestInferRepositoryFollowsRelativeGitdirWithoutCommondir(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "modules", "sub")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[remote \"origin\"]\n\turl = https://bitbucket.org/acme/sub.git\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "sub")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".git"), []byte("gitdir: ../modules/sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	workspace, repo, err := InferRepository()
	if err != nil || workspace != "acme" || repo != "sub" {
		t.Fatalf("InferRepository() = %q, %q, %v", workspace, repo, err)
	}
}
