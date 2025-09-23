package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kabilan108/atlas/internal/convert"
	"github.com/kabilan108/atlas/internal/httpclient"
	"github.com/kabilan108/atlas/internal/output"
)

type Client struct {
	httpClient *httpclient.Client
	baseURL    string
}

type RepositorySearchResult struct {
	Values []Repository `json:"values"`
	Size   int          `json:"size"`
}

type Repository struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	IsPrivate   bool      `json:"is_private"`
	Owner       User      `json:"owner"`
	UpdatedOn   string    `json:"updated_on"`
	Links       RepoLinks `json:"links"`
}

type User struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	UUID        string `json:"uuid"`
}

type RepoLinks struct {
	HTML struct {
		Href string `json:"href"`
	} `json:"html"`
}

type PullRequestSearchResult struct {
	Values []PullRequest `json:"values"`
	Size   int           `json:"size"`
}

type PullRequest struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	Author      User    `json:"author"`
	UpdatedOn   string  `json:"updated_on"`
	Source      Branch  `json:"source"`
	Destination Branch  `json:"destination"`
	Links       PRLinks `json:"links"`
}

type Branch struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Repository Repository `json:"repository"`
}

type PRLinks struct {
	HTML struct {
		Href string `json:"href"`
	} `json:"html"`
	Diff struct {
		Href string `json:"href"`
	} `json:"diff"`
}

func NewClient(baseURL string) (*Client, error) {
	httpClient, err := httpclient.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	if baseURL == "" {
		baseURL = "https://api.bitbucket.org/2.0"
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}, nil
}

func (c *Client) SearchRepositories(ctx context.Context, workspace string, query string, limit int) ([]output.Document, error) {
	params := url.Values{}
	if query != "" {
		params.Set("q", fmt.Sprintf("name~\"%s\"", query))
	}
	if limit > 0 {
		params.Set("pagelen", strconv.Itoa(limit))
	}

	var searchURL string
	if workspace != "" {
		searchURL = fmt.Sprintf("%s/repositories/%s?%s", c.baseURL, workspace, params.Encode())
	} else {
		searchURL = fmt.Sprintf("%s/repositories?%s", c.baseURL, params.Encode())
	}

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("repository search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("repository search failed with status %d", resp.StatusCode)
	}

	var result RepositorySearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var documents []output.Document
	for _, repo := range result.Values {
		doc := c.convertRepositoryToDocument(repo)
		documents = append(documents, *doc)
	}

	return documents, nil
}

func (c *Client) SearchPullRequests(ctx context.Context, workspace string, repo string, query string, limit int) ([]output.Document, error) {
	params := url.Values{}
	params.Set("state", "OPEN")
	if query != "" {
		params.Set("q", fmt.Sprintf("title~\"%s\" OR description~\"%s\"", query, query))
	}
	if limit > 0 {
		params.Set("pagelen", strconv.Itoa(limit))
	}

	searchURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests?%s", c.baseURL, workspace, repo, params.Encode())

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("PR search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PR search failed with status %d", resp.StatusCode)
	}

	var result PullRequestSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var documents []output.Document
	for _, pr := range result.Values {
		doc, err := c.convertPullRequestToDocument(pr, false)
		if err != nil {
			output.LogError("Failed to convert PR %d: %v", pr.ID, err)
			continue
		}
		documents = append(documents, *doc)
	}

	return documents, nil
}

func (c *Client) GetPullRequest(ctx context.Context, workspace string, repo string, prID int, includeDiff bool) (*output.Document, error) {
	prURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d", c.baseURL, workspace, repo, prID)

	req, err := http.NewRequest("GET", prURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("PR request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get PR failed with status %d", resp.StatusCode)
	}

	var pr PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.convertPullRequestToDocument(pr, includeDiff)
}

func (c *Client) GetPullRequestDiff(ctx context.Context, workspace string, repo string, prID int) (string, error) {
	diffURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/diff", c.baseURL, workspace, repo, prID)

	req, err := http.NewRequest("GET", diffURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("diff request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get diff failed with status %d", resp.StatusCode)
	}

	buf := make([]byte, resp.ContentLength)
	if _, err := resp.Body.Read(buf); err != nil && err.Error() != "EOF" {
		return "", fmt.Errorf("failed to read diff: %w", err)
	}

	return string(buf), nil
}

func (c *Client) convertRepositoryToDocument(repo Repository) *output.Document {
	var description string
	if repo.Description != "" {
		description = repo.Description
	} else {
		description = fmt.Sprintf("Repository: %s", repo.Name)
	}

	return &output.Document{
		Title:     repo.Name,
		URL:       repo.Links.HTML.Href,
		ID:        repo.FullName,
		Source:    "bitbucket",
		Workspace: repo.Owner.Username,
		Repo:      repo.Name,
		Author:    repo.Owner.DisplayName,
		UpdatedAt: repo.UpdatedOn,
		Content:   description,
	}
}

func (c *Client) convertPullRequestToDocument(pr PullRequest, includeDiff bool) (*output.Document, error) {
	var content strings.Builder

	if pr.Description != "" {
		markdown, err := convert.HtmlToMarkdown(pr.Description)
		if err != nil {
			content.WriteString(pr.Description)
		} else {
			content.WriteString(markdown)
		}
	}

	if includeDiff {
		diff, err := c.GetPullRequestDiff(context.Background(), pr.Source.Repository.Owner.Username, pr.Source.Repository.Name, pr.ID)
		if err != nil {
			output.LogError("Failed to fetch diff for PR %d: %v", pr.ID, err)
		} else {
			if content.Len() > 0 {
				content.WriteString("\n\n")
			}
			content.WriteString("## Diff\n\n```diff\n")
			content.WriteString(diff)
			content.WriteString("\n```")
		}
	}

	return &output.Document{
		Title:     pr.Title,
		URL:       pr.Links.HTML.Href,
		ID:        strconv.Itoa(pr.ID),
		Source:    "bitbucket",
		Workspace: pr.Source.Repository.Owner.Username,
		Repo:      pr.Source.Repository.Name,
		Path:      fmt.Sprintf("pullrequests/%d", pr.ID),
		Author:    pr.Author.DisplayName,
		UpdatedAt: pr.UpdatedOn,
		Content:   content.String(),
	}, nil
}
