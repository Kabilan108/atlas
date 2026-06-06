package bitbucket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/config"
)

const defaultBaseURL = "https://api.bitbucket.org/2.0"

type Client struct {
	httpClient *http.Client
	baseURL    string
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
		baseURL:    defaultBaseURL,
		username:   cfg.Username,
		password:   cfg.AppPassword,
		cache:      cache,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

func newClientForTest(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		username:   "user",
		password:   "password",
		noCache:    true,
	}
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
	requestURL := c.baseURL + path

	if !c.noCache {
		if data, ok := c.cache.Get(requestURL); ok {
			return data, nil
		}
	}

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
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
		c.cache.Set(requestURL, body)
	}

	return body, nil
}

func (c *Client) getRaw(path string) ([]byte, error) {
	requestURL := c.baseURL + path

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
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

func (c *Client) post(path string, body any) ([]byte, error) {
	return c.sendJSON(http.MethodPost, path, body)
}

func (c *Client) put(path string, body any) ([]byte, error) {
	return c.sendJSON(http.MethodPut, path, body)
}

func (c *Client) delete(path string) ([]byte, error) {
	return c.sendJSON(http.MethodDelete, path, nil)
}

func (c *Client) sendJSON(method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request body: %w", err)
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp, data); err != nil {
		return nil, err
	}
	return data, nil
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

func (c *Client) GetUser(identifier string) (*User, error) {
	path := "/users/" + url.PathEscape(identifier)
	data, err := c.get(path)
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
		path = c.extractNextPath(page.Next)
	}

	return repos, nil
}

func (c *Client) GetRepository(workspace, repo string) (*Repository, error) {
	path := fmt.Sprintf("/repositories/%s/%s", workspace, repo)
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var repository Repository
	if err := json.Unmarshal(data, &repository); err != nil {
		return nil, fmt.Errorf("failed to parse repository response: %w", err)
	}

	return &repository, nil
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
			if opts != nil && opts.Author != "" && !pr.Author.MatchesStableIdentifier(opts.Author) {
				continue
			}
			if opts != nil && opts.Reviewer != "" && !hasReviewer(pr, opts.Reviewer) {
				continue
			}
			prs = append(prs, pr)
		}
		path = c.extractNextPath(page.Next)
	}

	return prs, nil
}

func hasReviewer(pr PullRequest, reviewer string) bool {
	for _, r := range pr.Reviewers {
		if r.MatchesStableIdentifier(reviewer) {
			return true
		}
	}
	for _, p := range pr.Participants {
		if p.Role == "REVIEWER" && p.User.MatchesStableIdentifier(reviewer) {
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

func (c *Client) CreatePullRequest(workspace, repo string, input PullRequestCreate) (*PullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests", workspace, repo)
	data, err := c.post(path, input)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) UpdatePullRequest(workspace, repo string, id int, update PullRequestUpdate) (*PullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", workspace, repo, id)
	data, err := c.put(path, update)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) DeclinePullRequest(workspace, repo string, id int) (*PullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/decline", workspace, repo, id)
	data, err := c.post(path, nil)
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
		path = c.extractNextPath(page.Next)
	}

	return comments, nil
}

func (c *Client) CreatePullRequestComment(workspace, repo string, id int, input CommentCreate) (*Comment, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/comments", workspace, repo, id)
	data, err := c.post(path, input)
	if err != nil {
		return nil, err
	}

	var comment Comment
	if err := json.Unmarshal(data, &comment); err != nil {
		return nil, fmt.Errorf("failed to parse comment response: %w", err)
	}

	return &comment, nil
}

func (c *Client) CreatePullRequestCommentText(workspace, repo string, id int, body string) (*Comment, error) {
	return c.CreatePullRequestComment(workspace, repo, id, CommentCreate{
		Content: ContentInput{Raw: body},
	})
}

func (c *Client) ApprovePullRequest(workspace, repo string, id int) (*ReviewActionResult, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/approve", workspace, repo, id)
	data, err := c.post(path, nil)
	if err != nil {
		return nil, err
	}

	var result ReviewActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse approval response: %w", err)
	}

	return &result, nil
}

func (c *Client) UnapprovePullRequest(workspace, repo string, id int) error {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/approve", workspace, repo, id)
	_, err := c.delete(path)
	return err
}

func (c *Client) RequestPullRequestChanges(workspace, repo string, id int) (*ReviewActionResult, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/request-changes", workspace, repo, id)
	data, err := c.post(path, nil)
	if err != nil {
		return nil, err
	}

	var result ReviewActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse request changes response: %w", err)
	}

	return &result, nil
}

func (c *Client) ClearPullRequestChanges(workspace, repo string, id int) error {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/request-changes", workspace, repo, id)
	_, err := c.delete(path)
	return err
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
		path = c.extractNextPath(page.Next)
	}

	return tasks, nil
}

func (c *Client) extractNextPath(nextURL string) string {
	if nextURL == "" {
		return ""
	}
	if len(nextURL) > len(c.baseURL) && nextURL[:len(c.baseURL)] == c.baseURL {
		return nextURL[len(c.baseURL):]
	}
	return ""
}

func escapePathPreservingSlashes(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("snippet filename cannot be empty")
	}
	if strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("snippet filename %q cannot be absolute", path)
	}

	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" {
			return "", fmt.Errorf("snippet filename %q cannot contain empty path segments", path)
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("snippet filename %q cannot contain dot segments", path)
		}
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/"), nil
}

type SnippetListOptions struct {
	Limit int
	Role  string
}

func snippetListPath(workspace string, opts *SnippetListOptions) string {
	values := url.Values{}
	if opts != nil {
		if opts.Limit > 0 {
			pageLen := opts.Limit
			if pageLen > 100 {
				pageLen = 100
			}
			values.Set("pagelen", strconv.Itoa(pageLen))
		}
		if opts.Role != "" {
			values.Set("role", opts.Role)
		}
	}

	path := fmt.Sprintf("/snippets/%s", url.PathEscape(workspace))
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}

func (c *Client) ListSnippets(workspace string, opts *SnippetListOptions) ([]Snippet, error) {
	var snippets []Snippet
	path := snippetListPath(workspace, opts)

	for path != "" {
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}

		var page PaginatedResponse[Snippet]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse snippets response: %w", err)
		}

		snippets = append(snippets, page.Values...)
		if opts != nil && opts.Limit > 0 && len(snippets) >= opts.Limit {
			return snippets[:opts.Limit], nil
		}
		path = c.extractNextPath(page.Next)
	}

	return snippets, nil
}

func (c *Client) GetSnippet(workspace, id string) (*Snippet, error) {
	path := fmt.Sprintf("/snippets/%s/%s", url.PathEscape(workspace), url.PathEscape(id))
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var snippet Snippet
	if err := json.Unmarshal(data, &snippet); err != nil {
		return nil, fmt.Errorf("failed to parse snippet response: %w", err)
	}

	return &snippet, nil
}

func (c *Client) GetSnippetFileContent(workspace, id, filename string) ([]byte, error) {
	escapedFilename, err := escapePathPreservingSlashes(filename)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/snippets/%s/%s/files/%s", url.PathEscape(workspace), url.PathEscape(id), escapedFilename)
	return c.getRaw(path)
}

func (c *Client) CreateSnippet(workspace, title string, files map[string][]byte, isPrivate bool) (*Snippet, error) {
	requestURL := c.baseURL + fmt.Sprintf("/snippets/%s", url.PathEscape(workspace))

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("title", title); err != nil {
		return nil, fmt.Errorf("failed to write title field: %w", err)
	}

	if err := writer.WriteField("is_private", strconv.FormatBool(isPrivate)); err != nil {
		return nil, fmt.Errorf("failed to write is_private field: %w", err)
	}

	for filename, content := range files {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err := part.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write file content: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, requestURL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

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

	var snippet Snippet
	if err := json.Unmarshal(body, &snippet); err != nil {
		return nil, fmt.Errorf("failed to parse snippet response: %w", err)
	}

	return &snippet, nil
}

func (c *Client) UpdateSnippet(workspace, id, title string, addFiles map[string][]byte, removeFiles []string) error {
	requestURL := c.baseURL + fmt.Sprintf("/snippets/%s/%s", url.PathEscape(workspace), url.PathEscape(id))

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if title != "" {
		if err := writer.WriteField("title", title); err != nil {
			return fmt.Errorf("failed to write title field: %w", err)
		}
	}

	for filename, content := range addFiles {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			return fmt.Errorf("failed to create form file: %w", err)
		}
		if _, err := part.Write(content); err != nil {
			return fmt.Errorf("failed to write file content: %w", err)
		}
	}

	for _, filename := range removeFiles {
		if err := writer.WriteField("files", filename); err != nil {
			return fmt.Errorf("failed to write file removal field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, requestURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return checkResponse(resp, body)
}

func (c *Client) DeleteSnippet(workspace, id string) error {
	requestURL := c.baseURL + fmt.Sprintf("/snippets/%s/%s", url.PathEscape(workspace), url.PathEscape(id))

	req, err := http.NewRequest(http.MethodDelete, requestURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return checkResponse(resp, body)
}
