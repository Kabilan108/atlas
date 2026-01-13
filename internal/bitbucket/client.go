package bitbucket

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/config"
)

const baseURL = "https://api.bitbucket.org/2.0"

type Client struct {
	httpClient *http.Client
	username   string
	password   string
	cache      *Cache
	noCache    bool
	retry      bool
}

type ClientOption func(*Client)

func WithNoCache(noCache bool) ClientOption {
	return func(c *Client) {
		c.noCache = noCache
	}
}

func WithRetry(retry bool) ClientOption {
	return func(c *Client) {
		c.retry = retry
	}
}

func NewClient(opts ...ClientOption) (*Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Username == "" || cfg.AppPassword == "" {
		return nil, NewAuthError(401, "missing credentials in config")
	}

	cache, err := NewCache()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}

	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		username:   cfg.Username,
		password:   cfg.AppPassword,
		cache:      cache,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 && c.retry {
		resetTime := parseRateLimitReset(resp.Header)
		resp.Body.Close()

		waitDuration := time.Until(resetTime)
		if waitDuration > 0 {
			time.Sleep(waitDuration)
		}
		return c.httpClient.Do(req)
	}

	return resp, nil
}

func (c *Client) get(path string) ([]byte, error) {
	url := baseURL + path

	if !c.noCache {
		if data, ok := c.cache.Get(url); ok {
			return data, nil
		}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := checkResponse(resp, body); err != nil {
		return nil, err
	}

	if !c.noCache {
		c.cache.Set(url, body)
	}

	return body, nil
}

func (c *Client) getRaw(path string) ([]byte, error) {
	url := baseURL + path

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := checkResponse(resp, body); err != nil {
		return nil, err
	}

	return body, nil
}

func checkResponse(resp *http.Response, body []byte) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	switch resp.StatusCode {
	case 401:
		return NewAuthError(401, "invalid credentials")
	case 403:
		return NewAuthError(403, "access denied")
	case 404:
		return NewNotFoundError("resource", extractResource(resp.Request.URL.Path))
	case 429:
		resetTime := parseRateLimitReset(resp.Header)
		return NewRateLimitError(resetTime)
	default:
		if resp.StatusCode >= 500 {
			return NewServerError(resp.StatusCode, string(body))
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			Resource:   "api",
		}
	}
}

func parseRateLimitReset(header http.Header) time.Time {
	resetStr := header.Get("X-RateLimit-Reset")
	if resetStr == "" {
		return time.Now().Add(60 * time.Second)
	}

	resetUnix, err := strconv.ParseInt(resetStr, 10, 64)
	if err != nil {
		return time.Now().Add(60 * time.Second)
	}

	return time.Unix(resetUnix, 0)
}

func extractResource(path string) string {
	if len(path) > 50 {
		return path[:50] + "..."
	}
	return path
}

func (c *Client) GetCurrentUser() (*User, error) {
	data, err := c.get("/user")
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}

	return &user, nil
}

func (c *Client) ListRepositories(workspace string) ([]Repository, error) {
	var repos []Repository
	path := fmt.Sprintf("/repositories/%s", workspace)

	for path != "" {
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}

		var page PaginatedResponse[Repository]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse repositories response: %w", err)
		}

		repos = append(repos, page.Values...)
		path = extractNextPath(page.Next)
	}

	return repos, nil
}

func (c *Client) ListPullRequests(workspace, repo string, opts *PRListOptions) ([]PullRequest, error) {
	var prs []PullRequest
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests", workspace, repo)

	var queryParams []string
	if opts != nil {
		if opts.State != "" {
			queryParams = append(queryParams, "state="+opts.State)
		}
	}
	if len(queryParams) > 0 {
		path += "?" + strings.Join(queryParams, "&")
	}

	for path != "" {
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}

		var page PaginatedResponse[PullRequest]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse pull requests response: %w", err)
		}

		for _, pr := range page.Values {
			if opts != nil && opts.Author != "" && pr.Author.Username != opts.Author {
				continue
			}
			if opts != nil && opts.Reviewer != "" && !hasReviewer(pr, opts.Reviewer) {
				continue
			}
			prs = append(prs, pr)
		}
		path = extractNextPath(page.Next)
	}

	return prs, nil
}

func hasReviewer(pr PullRequest, reviewer string) bool {
	for _, r := range pr.Reviewers {
		if r.Username == reviewer {
			return true
		}
	}
	for _, p := range pr.Participants {
		if p.Role == "REVIEWER" && p.User.Username == reviewer {
			return true
		}
	}
	return false
}

func (c *Client) ListAllPullRequests(workspace string, opts *PRListOptions) ([]PullRequest, error) {
	repos, err := c.ListRepositories(workspace)
	if err != nil {
		return nil, err
	}

	var allPRs []PullRequest
	for _, repo := range repos {
		prs, err := c.ListPullRequests(workspace, repo.Name, opts)
		if err != nil {
			continue
		}
		allPRs = append(allPRs, prs...)
	}

	return allPRs, nil
}

func (c *Client) FindPullRequestByBranch(workspace, repo, branch string) (*PullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests?q=source.branch.name=\"%s\"", workspace, repo, branch)
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var page PaginatedResponse[PullRequest]
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("failed to parse pull requests response: %w", err)
	}

	if len(page.Values) == 0 {
		return nil, NewNotFoundError("pull request", fmt.Sprintf("branch %s", branch))
	}

	var selected *PullRequest
	for i := range page.Values {
		pr := &page.Values[i]
		if pr.State == "OPEN" {
			return pr, nil
		}
		if selected == nil || pr.UpdatedOn.After(selected.UpdatedOn) {
			selected = pr
		}
	}

	return selected, nil
}

func (c *Client) GetPullRequest(workspace, repo string, id int) (*PullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", workspace, repo, id)
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) ListPullRequestComments(workspace, repo string, id int) ([]Comment, error) {
	var comments []Comment
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/comments", workspace, repo, id)

	for path != "" {
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}

		var page PaginatedResponse[Comment]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse comments response: %w", err)
		}

		comments = append(comments, page.Values...)
		path = extractNextPath(page.Next)
	}

	return comments, nil
}

func (c *Client) GetPullRequestDiff(workspace, repo string, id int) ([]byte, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/diff", workspace, repo, id)
	return c.getRaw(path)
}

func (c *Client) ListPullRequestTasks(workspace, repo string, id int) ([]Task, error) {
	var tasks []Task
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/tasks", workspace, repo, id)

	for path != "" {
		data, err := c.get(path)
		if err != nil {
			if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 404 {
				return []Task{}, nil
			}
			return nil, err
		}

		var page PaginatedResponse[Task]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse tasks response: %w", err)
		}

		tasks = append(tasks, page.Values...)
		path = extractNextPath(page.Next)
	}

	return tasks, nil
}

func extractNextPath(nextURL string) string {
	if nextURL == "" {
		return ""
	}
	if len(nextURL) > len(baseURL) && nextURL[:len(baseURL)] == baseURL {
		return nextURL[len(baseURL):]
	}
	return ""
}
