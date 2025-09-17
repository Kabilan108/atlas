package parse

import (
	"testing"
)

func TestDetectURLType(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected URLType
	}{
		{
			name:     "Confluence URL with atlassian.net",
			url:      "https://company.atlassian.net/wiki/spaces/DEV/pages/123456",
			expected: URLTypeConfluence,
		},
		{
			name:     "Confluence URL with confluence keyword",
			url:      "https://confluence.company.com/wiki/pages/123456",
			expected: URLTypeConfluence,
		},
		{
			name:     "Bitbucket URL",
			url:      "https://bitbucket.org/workspace/repo/pull-requests/42",
			expected: URLTypeBitbucket,
		},
		{
			name:     "Unknown URL",
			url:      "https://github.com/user/repo",
			expected: URLTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectURLType(tt.url)
			if result != tt.expected {
				t.Errorf("DetectURLType(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestParseConfluenceURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		expected    *ConfluencePageInfo
	}{
		{
			name:        "Standard Confluence URL with space",
			url:         "https://company.atlassian.net/wiki/spaces/DEV/pages/123456",
			expectError: false,
			expected: &ConfluencePageInfo{
				PageID:  "123456",
				Space:   "DEV",
				BaseURL: "https://company.atlassian.net",
			},
		},
		{
			name:        "Confluence URL without space",
			url:         "https://company.atlassian.net/wiki/pages/123456",
			expectError: false,
			expected: &ConfluencePageInfo{
				PageID:  "123456",
				Space:   "",
				BaseURL: "https://company.atlassian.net",
			},
		},
		{
			name:        "Confluence URL with viewpage action",
			url:         "https://company.atlassian.net/wiki/spaces/DEV/pages/viewpage.action?pageId=123456",
			expectError: false,
			expected: &ConfluencePageInfo{
				PageID:  "123456",
				Space:   "DEV",
				BaseURL: "https://company.atlassian.net",
			},
		},
		{
			name:        "Invalid URL without page ID",
			url:         "https://company.atlassian.net/wiki/spaces/DEV",
			expectError: true,
			expected:    nil,
		},
		{
			name:        "Invalid URL format",
			url:         "not-a-url",
			expectError: true,
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseConfluenceURL(tt.url)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseConfluenceURL(%q) expected error but got none", tt.url)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseConfluenceURL(%q) unexpected error: %v", tt.url, err)
				return
			}

			if result.PageID != tt.expected.PageID {
				t.Errorf("PageID = %q, want %q", result.PageID, tt.expected.PageID)
			}

			if result.Space != tt.expected.Space {
				t.Errorf("Space = %q, want %q", result.Space, tt.expected.Space)
			}

			if result.BaseURL != tt.expected.BaseURL {
				t.Errorf("BaseURL = %q, want %q", result.BaseURL, tt.expected.BaseURL)
			}
		})
	}
}

func TestParseBitbucketPR(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expected    *BitbucketPRInfo
	}{
		{
			name:        "Bitbucket URL format",
			input:       "https://bitbucket.org/repositories/workspace/repo/pull-requests/42",
			expectError: false,
			expected: &BitbucketPRInfo{
				Workspace: "workspace",
				Repo:      "repo",
				PRID:      42,
				BaseURL:   "https://api.bitbucket.org/2.0",
			},
		},
		{
			name:        "Shorthand format",
			input:       "workspace/repo#42",
			expectError: false,
			expected: &BitbucketPRInfo{
				Workspace: "workspace",
				Repo:      "repo",
				PRID:      42,
				BaseURL:   "https://api.bitbucket.org/2.0",
			},
		},
		{
			name:        "Shorthand format with dashes",
			input:       "my-workspace/my-repo#123",
			expectError: false,
			expected: &BitbucketPRInfo{
				Workspace: "my-workspace",
				Repo:      "my-repo",
				PRID:      123,
				BaseURL:   "https://api.bitbucket.org/2.0",
			},
		},
		{
			name:        "Invalid shorthand format",
			input:       "workspace/repo",
			expectError: true,
			expected:    nil,
		},
		{
			name:        "Invalid PR ID",
			input:       "workspace/repo#abc",
			expectError: true,
			expected:    nil,
		},
		{
			name:        "Invalid URL",
			input:       "https://github.com/user/repo/pull/42",
			expectError: true,
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseBitbucketPR(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseBitbucketPR(%q) expected error but got none", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseBitbucketPR(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result.Workspace != tt.expected.Workspace {
				t.Errorf("Workspace = %q, want %q", result.Workspace, tt.expected.Workspace)
			}

			if result.Repo != tt.expected.Repo {
				t.Errorf("Repo = %q, want %q", result.Repo, tt.expected.Repo)
			}

			if result.PRID != tt.expected.PRID {
				t.Errorf("PRID = %d, want %d", result.PRID, tt.expected.PRID)
			}

			if result.BaseURL != tt.expected.BaseURL {
				t.Errorf("BaseURL = %q, want %q", result.BaseURL, tt.expected.BaseURL)
			}
		})
	}
}

func TestIsValidConfluencePageID(t *testing.T) {
	tests := []struct {
		name     string
		pageID   string
		expected bool
	}{
		{
			name:     "Valid numeric page ID",
			pageID:   "123456",
			expected: true,
		},
		{
			name:     "Empty page ID",
			pageID:   "",
			expected: false,
		},
		{
			name:     "Non-numeric page ID",
			pageID:   "abc123",
			expected: false,
		},
		{
			name:     "Page ID with spaces",
			pageID:   "123 456",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidConfluencePageID(tt.pageID)
			if result != tt.expected {
				t.Errorf("IsValidConfluencePageID(%q) = %v, want %v", tt.pageID, result, tt.expected)
			}
		})
	}
}
