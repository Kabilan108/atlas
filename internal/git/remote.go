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

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotGitRepository
		}
		dir = parent
	}
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
