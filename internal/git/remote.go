package git

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrNotGitRepository   = errors.New("not a git repository")
	ErrNoOriginRemote     = errors.New("no origin remote configured")
	ErrNotBitbucketRemote = errors.New("remote is not a Bitbucket URL")
	ErrInvalidRemoteURL   = errors.New("invalid remote URL format")
)

var (
	sshPattern   = regexp.MustCompile(`^git@bitbucket\.org:([^/]+)/([^/]+?)(?:\.git)?$`)
	httpsPattern = regexp.MustCompile(`^https://(?:[^@]+@)?bitbucket\.org/([^/]+)/([^/]+?)(?:\.git)?$`)
)

func InferRepository() (workspace string, repo string, err error) {
	gitDir, err := findGitDir()
	if err != nil {
		return "", "", err
	}

	url, err := getOriginURL(gitDir)
	if err != nil {
		return "", "", err
	}

	return ParseRemoteURL(url)
}

func ParseRemoteURL(url string) (workspace string, repo string, err error) {
	url = strings.TrimSpace(url)

	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	if strings.Contains(url, "bitbucket.org") {
		return "", "", ErrInvalidRemoteURL
	}

	return "", "", ErrNotBitbucketRemote
}

// findGitDir returns the directory holding the repository's config for the
// working directory. In a linked worktree or a submodule .git is a file
// naming the real git directory, and a worktree's config lives in the common
// directory that git directory points to.
func findGitDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil && info.IsDir() {
			return gitPath, nil
		}
		if err == nil && info.Mode().IsRegular() {
			return gitDirFromFile(gitPath)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotGitRepository
		}
		dir = parent
	}
}

// gitDirFromFile follows a "gitdir: <path>" file, then the git directory's
// commondir file when it has one.
func gitDirFromFile(gitFile string) (string, error) {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", gitFile, err)
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("%s is not a gitdir file", gitFile)
	}
	gitDir := resolveFrom(filepath.Dir(gitFile), strings.TrimSpace(target))

	common, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, os.ErrNotExist) {
		return gitDir, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", filepath.Join(gitDir, "commondir"), err)
	}
	return resolveFrom(gitDir, strings.TrimSpace(string(common))), nil
}

func resolveFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func getOriginURL(gitDir string) (string, error) {
	configPath := filepath.Join(gitDir, "config")
	file, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read git config: %w", err)
	}
	defer file.Close()

	var inOriginSection bool
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[") {
			inOriginSection = line == `[remote "origin"]`
			continue
		}

		if inOriginSection && strings.HasPrefix(line, "url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to parse git config: %w", err)
	}

	return "", ErrNoOriginRemote
}
