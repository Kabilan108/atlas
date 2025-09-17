package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/httpclient"
	"github.com/kabilan108/atlas/internal/parse"
)

const (
	baseURLEnv           = "ATLAS_BITBUCKET_BASE_URL"
	defaultBitbucketBase = "https://api.bitbucket.org/2.0"
)

// Doer represents the HTTP behaviour expected from the shared client.
type Doer interface {
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Client wraps Bitbucket REST interactions.
type Client struct {
	doer    Doer
	apiBase *url.URL
}

// Repository models a Bitbucket repository.
type Repository struct {
	Slug     string
	Name     string
	FullName string
	WebURL   string
}

// PullRequestSummary captures the minimal fields returned from a search.
type PullRequestSummary struct {
	ID          int
	Title       string
	Description string
	State       string
	Author      string
	Updated     time.Time
	WebURL      string
	Workspace   string
	RepoSlug    string
}

// PullRequest describes a detailed Bitbucket pull request.
type PullRequest struct {
	PullRequestSummary
	SourceBranch      string
	DestinationBranch string
	Diff              string
}

// SearchPROptions configures pull request searches.
type SearchPROptions struct {
	Query         string
	Repo          string
	Limit         int
	IncludeClosed bool
}

// NewClient constructs a Bitbucket client with the provided HTTP doer.
func NewClient(doer Doer) (*Client, error) {
	if doer == nil {
		return nil, errors.New("bitbucket client requires a Doer")
	}

	rawBase := strings.TrimSpace(os.Getenv(baseURLEnv))
	if rawBase == "" {
		rawBase = defaultBitbucketBase
	}

	apiBase, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("parse bitbucket base URL: %w", err)
	}
	if apiBase.Scheme == "" {
		apiBase.Scheme = "https"
	}

	return &Client{doer: doer, apiBase: apiBase}, nil
}

// ListRepositories returns repositories for the workspace (optionally limited).
func (c *Client) ListRepositories(ctx context.Context, workspace string, limit int) ([]Repository, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, errors.New("workspace is required")
	}

	endpoint := c.apiBase.JoinPath("repositories", workspace)
	params := endpoint.Query()
	params.Set("pagelen", "50")
	endpoint.RawQuery = params.Encode()

	var repositories []Repository
	nextURL := endpoint.String()

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}

		resp, err := c.doer.Do(ctx, req)
		if err != nil {
			return nil, err
		}

		page, err := decodeRepoPage(resp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		for _, value := range page.Values {
			repositories = append(repositories, Repository{
				Slug:     value.Slug,
				Name:     value.Name,
				FullName: value.FullName,
				WebURL:   value.Links.HTML.Href,
			})
			if limit > 0 && len(repositories) >= limit {
				return repositories[:limit], nil
			}
		}
		nextURL = page.Next
	}

	return repositories, nil
}

// SearchPullRequests searches pull requests across the workspace (optionally scoped to a repo).
func (c *Client) SearchPullRequests(ctx context.Context, workspace string, opts SearchPROptions) ([]PullRequestSummary, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, errors.New("workspace is required")
	}

	var repos []Repository
	var err error
	if opts.Repo != "" {
		repos = []Repository{{Slug: opts.Repo}}
	} else {
		repos, err = c.ListRepositories(ctx, workspace, 0)
		if err != nil {
			return nil, err
		}
	}

	var results []PullRequestSummary
	for _, repo := range repos {
		summaries, err := c.fetchPullRequests(ctx, workspace, repo.Slug, opts)
		if err != nil {
			return nil, err
		}
		results = append(results, summaries...)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			return results[:opts.Limit], nil
		}
	}

	return results, nil
}

// GetPullRequest retrieves a single pull request and optionally the diff.
func (c *Client) GetPullRequest(ctx context.Context, ref parse.PullRequestRef, includeDiff bool) (*PullRequest, error) {
	endpoint := c.apiBase.JoinPath("repositories", ref.Workspace, ref.RepoSlug, "pullrequests", strconv.Itoa(ref.ID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.doer.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	pr, err := decodePullRequest(resp)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	summary := PullRequestSummary{
		ID:          pr.ID,
		Title:       pr.Title,
		Description: pr.Summary.Raw,
		State:       pr.State,
		Author:      pr.Author.DisplayName,
		Updated:     parseTime(pr.UpdatedOn),
		WebURL:      pr.Links.HTML.Href,
		Workspace:   ref.Workspace,
		RepoSlug:    ref.RepoSlug,
	}

	result := &PullRequest{
		PullRequestSummary: summary,
		SourceBranch:       pr.Source.Branch.Name,
		DestinationBranch:  pr.Destination.Branch.Name,
	}

	if includeDiff {
		diffEndpoint := endpoint.JoinPath("diff")
		diffReq, err := http.NewRequestWithContext(ctx, http.MethodGet, diffEndpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("new diff request: %w", err)
		}

		diffResp, err := c.doer.Do(ctx, diffReq)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(diffResp.Body)
		diffResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read diff: %w", err)
		}
		if diffResp.StatusCode >= 400 {
			return nil, fmt.Errorf("fetch diff: status %d", diffResp.StatusCode)
		}
		result.Diff = string(body)
	}

	return result, nil
}

func (c *Client) fetchPullRequests(ctx context.Context, workspace, repo string, opts SearchPROptions) ([]PullRequestSummary, error) {
	endpoint := c.apiBase.JoinPath("repositories", workspace, repo, "pullrequests")
	params := endpoint.Query()
	params.Set("pagelen", "50")
	if opts.Query != "" {
		params.Set("q", buildPRQuery(opts.Query, opts.IncludeClosed))
	} else if !opts.IncludeClosed {
		params.Set("q", "state = \"OPEN\"")
	}
	endpoint.RawQuery = params.Encode()

	var summaries []PullRequestSummary
	nextURL := endpoint.String()

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}

		resp, err := c.doer.Do(ctx, req)
		if err != nil {
			return nil, err
		}

		page, err := decodePRPage(resp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		for _, value := range page.Values {
			summary := PullRequestSummary{
				ID:          value.ID,
				Title:       value.Title,
				Description: value.Summary.Raw,
				State:       value.State,
				Author:      value.Author.DisplayName,
				Updated:     parseTime(value.UpdatedOn),
				WebURL:      value.Links.HTML.Href,
				Workspace:   workspace,
				RepoSlug:    repo,
			}

			if opts.Query == "" || matchQuery(summary, opts.Query) {
				summaries = append(summaries, summary)
			}

			if opts.Limit > 0 && len(summaries) >= opts.Limit {
				return summaries[:opts.Limit], nil
			}
		}

		nextURL = page.Next
	}

	return summaries, nil
}

func buildPRQuery(query string, includeClosed bool) string {
	escaped := strings.ReplaceAll(query, "\"", "\\\"")
	clauses := []string{fmt.Sprintf("(title ~ \"%s\" OR summary.raw ~ \"%s\")", escaped, escaped)}
	if !includeClosed {
		clauses = append(clauses, "state = \"OPEN\"")
	}
	return strings.Join(clauses, " AND ")
}

func matchQuery(pr PullRequestSummary, query string) bool {
	if query == "" {
		return true
	}
	lowered := strings.ToLower(query)
	return strings.Contains(strings.ToLower(pr.Title), lowered) || strings.Contains(strings.ToLower(pr.Description), lowered)
}

func decodeRepoPage(resp *http.Response) (*repoPage, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list repositories: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var page repoPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decode repositories: %w", err)
	}
	return &page, nil
}

func decodePRPage(resp *http.Response) (*pullRequestPage, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list pull requests: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var page pullRequestPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decode pull requests: %w", err)
	}
	return &page, nil
}

func decodePullRequest(resp *http.Response) (*pullRequestResponse, error) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get pull request: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var pr pullRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decode pull request: %w", err)
	}
	return &pr, nil
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04:05.000000+00:00"}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts
		}
	}
	return time.Time{}
}

var _ Doer = (*httpclient.Client)(nil)

type repoPage struct {
	Next   string `json:"next"`
	Values []struct {
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Links    struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	} `json:"values"`
}

type pullRequestPage struct {
	Next   string `json:"next"`
	Values []struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Summary struct {
			Raw string `json:"raw"`
		} `json:"summary"`
		State  string `json:"state"`
		Author struct {
			DisplayName string `json:"display_name"`
		} `json:"author"`
		UpdatedOn string `json:"updated_on"`
		Links     struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	} `json:"values"`
}

type pullRequestResponse struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Summary struct {
		Raw string `json:"raw"`
	} `json:"summary"`
	State  string `json:"state"`
	Author struct {
		DisplayName string `json:"display_name"`
	} `json:"author"`
	UpdatedOn string `json:"updated_on"`
	Source    struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}
