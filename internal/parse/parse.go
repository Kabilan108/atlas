package parse

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	confluencePageRegex  = regexp.MustCompile(`/wiki/(?:spaces/[^/]+/)?pages/(?:viewpage\.action\?pageId=)?(\d+)`)
	confluenceSpaceRegex = regexp.MustCompile(`/wiki/spaces/([^/]+)`)
	bitbucketPRRegex     = regexp.MustCompile(`^([^/]+)/([^#]+)#(\d+)$`)
	bitbucketURLRegex    = regexp.MustCompile(`/repositories/([^/]+)/([^/]+)/pull-requests/(\d+)`)
)

type ConfluencePageInfo struct {
	PageID  string
	Space   string
	BaseURL string
}

type BitbucketPRInfo struct {
	Workspace string
	Repo      string
	PRID      int
	BaseURL   string
}

type URLType int

const (
	URLTypeUnknown URLType = iota
	URLTypeConfluence
	URLTypeBitbucket
)

func DetectURLType(rawURL string) URLType {
	if strings.Contains(rawURL, "atlassian.net/wiki") || strings.Contains(rawURL, "confluence") {
		return URLTypeConfluence
	}
	if strings.Contains(rawURL, "bitbucket.org") || strings.Contains(rawURL, "pull-requests") {
		return URLTypeBitbucket
	}
	return URLTypeUnknown
}

func ParseConfluenceURL(rawURL string) (*ConfluencePageInfo, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	var pageID string

	// First try to extract from path
	matches := confluencePageRegex.FindStringSubmatch(parsedURL.Path)
	if len(matches) >= 2 {
		pageID = matches[1]
	} else {
		// Try to extract from query parameters (for viewpage.action URLs)
		query := parsedURL.Query()
		if pid := query.Get("pageId"); pid != "" {
			pageID = pid
		} else {
			return nil, fmt.Errorf("could not extract page ID from URL: %s", rawURL)
		}
	}

	var space string
	spaceMatches := confluenceSpaceRegex.FindStringSubmatch(parsedURL.Path)
	if len(spaceMatches) >= 2 {
		space = spaceMatches[1]
	}

	return &ConfluencePageInfo{
		PageID:  pageID,
		Space:   space,
		BaseURL: baseURL,
	}, nil
}

func ParseBitbucketPR(input string) (*BitbucketPRInfo, error) {
	input = strings.TrimSpace(input)

	if strings.HasPrefix(input, "http") {
		return parseBitbucketURL(input)
	}

	return parseBitbucketShorthand(input)
}

func parseBitbucketURL(rawURL string) (*BitbucketPRInfo, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	baseURL := "https://api.bitbucket.org/2.0"

	matches := bitbucketURLRegex.FindStringSubmatch(parsedURL.Path)
	if len(matches) < 4 {
		return nil, fmt.Errorf("could not extract PR info from URL: %s", rawURL)
	}

	workspace := matches[1]
	repo := matches[2]
	prID, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid PR ID: %s", matches[3])
	}

	return &BitbucketPRInfo{
		Workspace: workspace,
		Repo:      repo,
		PRID:      prID,
		BaseURL:   baseURL,
	}, nil
}

func parseBitbucketShorthand(input string) (*BitbucketPRInfo, error) {
	matches := bitbucketPRRegex.FindStringSubmatch(input)
	if len(matches) < 4 {
		return nil, fmt.Errorf("invalid format, expected workspace/repo#id, got: %s", input)
	}

	workspace := matches[1]
	repo := matches[2]
	prID, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid PR ID: %s", matches[3])
	}

	return &BitbucketPRInfo{
		Workspace: workspace,
		Repo:      repo,
		PRID:      prID,
		BaseURL:   "https://api.bitbucket.org/2.0",
	}, nil
}

func IsValidConfluencePageID(pageID string) bool {
	_, err := strconv.Atoi(pageID)
	return err == nil && len(pageID) > 0
}
