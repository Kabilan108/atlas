package parse

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// ConfluencePageID extracts the numeric page identifier from common Confluence URLs or accepts plain IDs.
func ConfluencePageID(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("confluence reference is empty")
	}

	if isDigits(trimmed) {
		return trimmed, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", errors.New("unable to parse confluence URL")
	}

	if pageID := parsed.Query().Get("pageId"); isDigits(pageID) {
		return pageID, nil
	}

	if fragmentID := extractFragmentPageID(parsed.Fragment); fragmentID != "" {
		return fragmentID, nil
	}

	segments := splitPath(parsed.Path)
	for i := 0; i < len(segments); i++ {
		seg := segments[i]
		if seg == "pages" && i+1 < len(segments) {
			candidate := segments[i+1]
			if isDigits(candidate) {
				return candidate, nil
			}
		}
	}

	return "", errors.New("could not locate confluence page id")
}

// PullRequestRef describes a Bitbucket pull request locator.
type PullRequestRef struct {
	Workspace string
	RepoSlug  string
	ID        int
}

// ParsePullRequestRef normalises a Bitbucket pull-request reference into its components.
func ParsePullRequestRef(input string) (PullRequestRef, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return PullRequestRef{}, errors.New("pull request reference is empty")
	}

	if strings.Contains(trimmed, "#") && !strings.Contains(trimmed, "://") {
		parts := strings.SplitN(trimmed, "#", 2)
		repo := strings.TrimSuffix(parts[0], "/")
		idPart := strings.TrimSpace(parts[1])
		repoParts := strings.Split(repo, "/")
		if len(repoParts) != 2 {
			return PullRequestRef{}, errors.New("invalid workspace/repo format")
		}
		id, err := strconv.Atoi(idPart)
		if err != nil {
			return PullRequestRef{}, errors.New("pull request id must be numeric")
		}
		return PullRequestRef{Workspace: repoParts[0], RepoSlug: repoParts[1], ID: id}, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return PullRequestRef{}, errors.New("unable to parse bitbucket URL")
	}

	segments := splitPath(parsed.Path)

	for i := 0; i < len(segments); i++ {
		seg := segments[i]
		if seg != "pull-requests" && seg != "pullrequests" {
			continue
		}
		if i+1 >= len(segments) {
			continue
		}
		idPart := segments[i+1]
		if !isDigits(idPart) {
			continue
		}
		if i < 2 {
			continue
		}

		workspace := segments[i-2]
		repo := segments[i-1]
		if workspace == "" || repo == "" {
			continue
		}
		id, err := strconv.Atoi(idPart)
		if err != nil {
			continue
		}
		return PullRequestRef{Workspace: workspace, RepoSlug: repo, ID: id}, nil
	}

	return PullRequestRef{}, errors.New("could not locate pull request identifier")
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func extractFragmentPageID(fragment string) string {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return ""
	}

	if strings.Contains(fragment, "=") {
		if values, err := url.ParseQuery(fragment); err == nil {
			if id := values.Get("pageId"); isDigits(id) {
				return id
			}
		}
	}

	if strings.HasPrefix(fragment, "pageId=") {
		candidate := strings.TrimPrefix(fragment, "pageId=")
		if isDigits(candidate) {
			return candidate
		}
	}

	if isDigits(fragment) {
		return fragment
	}

	return ""
}
