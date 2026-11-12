package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/redact"
)

const defaultBaseURL = "https://api.bitbucket.org/2.0"

type Client struct {
	httpClient *http.Client
	baseURL    string
	username   string
	password   string
	// cache is nil unless WithCache set one; without it the client never
	// touches the disk.
	cache *Cache
	retry bool
	// redactor scrubs the credentials from anything a server sends back
	// that ends up in an error.
	redactor *redact.Credentials
}

type ClientOption func(*Client)

// WithCache caches GET responses in cache, keyed by username and URL, and
// clears it after writes.
func WithCache(cache *Cache) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

// WithRetry waits for the rate-limit reset and retries once when Bitbucket
// answers 429. The wait ends early when the request's context does.
func WithRetry(retry bool) ClientOption {
	return func(c *Client) {
		c.retry = retry
	}
}

func NewClientWithCredentials(username, apiToken string, opts ...ClientOption) (*Client, error) {
	if username == "" || apiToken == "" {
		return nil, NewAuthError(401, "missing credentials")
	}

	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    defaultBaseURL,
		username:   username,
		password:   apiToken,
	}

	for _, opt := range opts {
		opt(c)
	}
	c.redactor = redact.Basic(username, apiToken)

	return c, nil
}

func newClientForTest(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		username:   "user",
		password:   "password",
		redactor:   redact.Basic("user", "password"),
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/json")
	return c.doAuthenticated(req)
}

// doAuthenticated sends req with the client's credentials. The rate-limit
// wait and retry stop when the request's context ends.
func (c *Client) doAuthenticated(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(c.username, c.password)
	httpClient := c.authenticatedHTTPClient()

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, c.redactError(err)
	}

	if resp.StatusCode == 429 && c.retry {
		resetTime := parseRateLimitReset(resp.Header)
		resp.Body.Close()

		if err := sleepContext(req.Context(), time.Until(resetTime)); err != nil {
			return nil, err
		}
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := httpClient.Do(req)
		return resp, c.redactError(err)
	}

	return resp, nil
}

// authenticatedHTTPClient copies the configured client, including one injected
// with WithHTTPClient, and refuses redirects that leave the base URL before its
// own policy runs. Go's default policy forwards Authorization to the same host
// on any port and to subdomains, which would leak credentials.
func (c *Client) authenticatedHTTPClient() *http.Client {
	clone := *c.httpClient
	callerPolicy := c.httpClient.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, ok := c.apiScopePath(req.URL); !ok {
			return fmt.Errorf("refusing redirect outside %s: %s", c.baseURL, req.URL.Redacted())
		}
		if callerPolicy != nil {
			return callerPolicy(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	requestURL := c.baseURL + path
	cacheKey := c.username + "\x00" + requestURL

	if c.cache.serves() {
		if data, ok := c.cache.Get(cacheKey); ok {
			return data, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}

	if err := c.checkResponse(resp, body); err != nil {
		return nil, err
	}

	if c.cache.serves() {
		c.cache.Set(cacheKey, body)
	}

	return body, nil
}

func (c *Client) getRaw(ctx context.Context, path string) ([]byte, error) {
	requestURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doAuthenticated(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}

	if err := c.checkResponse(resp, body); err != nil {
		return nil, err
	}

	return body, nil
}

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.sendJSON(ctx, http.MethodPost, path, body)
}

func (c *Client) put(ctx context.Context, path string, body any) ([]byte, error) {
	return c.sendJSON(ctx, http.MethodPut, path, body)
}

func (c *Client) delete(ctx context.Context, path string) ([]byte, error) {
	return c.sendJSON(ctx, http.MethodDelete, path, nil)
}

func (c *Client) sendJSON(ctx context.Context, method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request body: %w", err)
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
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

	data, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if err := c.checkResponse(resp, data); err != nil {
		return nil, err
	}
	return data, nil
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

func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	data, err := c.get(ctx, "/user")
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}

	return &user, nil
}

func (c *Client) GetUser(ctx context.Context, identifier string) (*User, error) {
	path := "/users/" + url.PathEscape(identifier)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}

	return &user, nil
}

func (c *Client) ListRepositories(ctx context.Context, workspace string) ([]Repository, error) {
	var repos []Repository
	path := "/repositories/" + url.PathEscape(workspace)

	for pages := 0; path != ""; pages++ {
		if pages == maxListPages || len(repos) >= MaxListItems {
			return nil, fmt.Errorf("workspace %s has more than %d repositories; name the repos to list instead", workspace, len(repos))
		}
		data, err := c.get(ctx, path)
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

func (c *Client) GetRepository(ctx context.Context, workspace, repo string) (*Repository, error) {
	path := repoPath(workspace, repo)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var repository Repository
	if err := json.Unmarshal(data, &repository); err != nil {
		return nil, fmt.Errorf("failed to parse repository response: %w", err)
	}

	return &repository, nil
}

// MaxPRListLimit is the most pull requests one ListPullRequests call
// returns, and maxPRListScan the most it reads while filtering by author or
// reviewer, so no listing walks a repository's whole history.
const (
	MaxPRListLimit = 500
	maxPRListScan  = 1000
	prPageLen      = 50
)

// PullRequestList is one repository's pull requests, newest updated first.
type PullRequestList struct {
	PullRequests []PullRequest
	// More is set when the listing stopped at its limit, or after reading
	// maxPRListScan pull requests, with more left on the server.
	More bool
}

// ListPullRequests lists a repository's pull requests newest updated first,
// reading pages only until opts.Limit match or the pull requests get older
// than opts.UpdatedSince.
func (c *Client) ListPullRequests(ctx context.Context, workspace, repo string, opts *PRListOptions) (*PullRequestList, error) {
	var o PRListOptions
	if opts != nil {
		o = *opts
	}
	limit := o.Limit
	if limit <= 0 || limit > MaxPRListLimit {
		limit = MaxPRListLimit
	}
	filtered := o.Author != "" || o.Reviewer != ""
	pageLen := prPageLen
	if !filtered {
		pageLen = min(limit, prPageLen)
	}
	values := url.Values{}
	for _, state := range o.States {
		values.Add("state", state)
	}
	if !o.UpdatedSince.IsZero() {
		values.Set("q", "updated_on >= "+o.UpdatedSince.UTC().Format("2006-01-02T15:04:05-07:00"))
	}
	values.Set("sort", "-updated_on")
	values.Set("pagelen", strconv.Itoa(pageLen))
	path := withQuery(repoPath(workspace, repo)+"/pullrequests", values)

	list := &PullRequestList{}
	scanned := 0
	visited := map[string]bool{}
	for path != "" {
		if visited[path] {
			return nil, fmt.Errorf("pull requests pagination did not advance: %s", path)
		}
		visited[path] = true
		data, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}

		var page PaginatedResponse[PullRequest]
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse pull requests response: %w", err)
		}

		for _, pr := range page.Values {
			if !o.UpdatedSince.IsZero() && pr.UpdatedOn.Before(o.UpdatedSince) {
				return list, nil
			}
			scanned++
			if o.Author != "" && !pr.Author.MatchesStableIdentifier(o.Author) {
				continue
			}
			if o.Reviewer != "" && !hasReviewer(pr, o.Reviewer) {
				continue
			}
			if len(list.PullRequests) == limit {
				list.More = true
				return list, nil
			}
			list.PullRequests = append(list.PullRequests, pr)
		}
		path = c.extractNextPath(page.Next)
		if path != "" && (len(list.PullRequests) == limit || scanned >= maxPRListScan) {
			list.More = true
			return list, nil
		}
	}

	return list, nil
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

// RepositoryError is one repository whose pull requests could not be listed.
type RepositoryError struct {
	Repository string
	Err        error
}

// RepositoryListError reports the repositories a multi-repository listing
// could not read; it comes with the results of every repository that did
// answer.
type RepositoryListError struct {
	Repositories int
	Failed       []RepositoryError
}

func (e *RepositoryListError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "failed to list pull requests in %d of %d repositories:", len(e.Failed), e.Repositories)
	for _, failed := range e.Failed {
		message, _, _ := strings.Cut(failed.Err.Error(), "\n")
		fmt.Fprintf(&b, "\n  %s: %s", failed.Repository, message)
	}
	return b.String()
}

func (c *Client) FindPullRequestByBranch(ctx context.Context, workspace, repo, branch string) (*PullRequest, error) {
	path := withQuery(repoPath(workspace, repo)+"/pullrequests", url.Values{"q": {"source.branch.name=" + bbqlString(branch)}})
	data, err := c.get(ctx, path)
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

func (c *Client) GetPullRequest(ctx context.Context, workspace, repo string, id int) (*PullRequest, error) {
	path := fmt.Sprintf("%s/pullrequests/%d", repoPath(workspace, repo), id)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) CreatePullRequest(ctx context.Context, workspace, repo string, input PullRequestCreate) (*PullRequest, error) {
	path := repoPath(workspace, repo) + "/pullrequests"
	data, err := c.post(ctx, path, input)
	if err != nil {
		return nil, err
	}

	c.invalidatePRCache()

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) UpdatePullRequest(ctx context.Context, workspace, repo string, id int, update PullRequestUpdate) (*PullRequest, error) {
	path := fmt.Sprintf("%s/pullrequests/%d", repoPath(workspace, repo), id)
	data, err := c.put(ctx, path, update)
	if err != nil {
		return nil, err
	}

	c.invalidatePRCache()

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

func (c *Client) DeclinePullRequest(ctx context.Context, workspace, repo string, id int) (*PullRequest, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/decline", repoPath(workspace, repo), id)
	data, err := c.post(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}

	return &pr, nil
}

// ListPullRequestComments returns up to limit comments (zero means
// MaxListItems); the Scan says whether there are more.
func (c *Client) ListPullRequestComments(ctx context.Context, workspace, repo string, id, limit int) ([]Comment, Scan, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/comments", repoPath(workspace, repo), id)
	return collectCachedPages[Comment](ctx, c, path, limit, "comments")
}

func (c *Client) CreatePullRequestComment(ctx context.Context, workspace, repo string, id int, input CommentCreate) (*Comment, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/comments", repoPath(workspace, repo), id)
	data, err := c.post(ctx, path, input)
	if err != nil {
		return nil, err
	}

	var comment Comment
	if err := json.Unmarshal(data, &comment); err != nil {
		return nil, fmt.Errorf("failed to parse comment response: %w", err)
	}

	return &comment, nil
}

func (c *Client) CreatePullRequestCommentText(ctx context.Context, workspace, repo string, id int, body string) (*Comment, error) {
	return c.CreatePullRequestComment(ctx, workspace, repo, id, CommentCreate{
		Content: ContentInput{Raw: body},
	})
}

func (c *Client) ApprovePullRequest(ctx context.Context, workspace, repo string, id int) (*ReviewActionResult, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/approve", repoPath(workspace, repo), id)
	data, err := c.post(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var result ReviewActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse approval response: %w", err)
	}

	return &result, nil
}

func (c *Client) UnapprovePullRequest(ctx context.Context, workspace, repo string, id int) error {
	path := fmt.Sprintf("%s/pullrequests/%d/approve", repoPath(workspace, repo), id)
	_, err := c.delete(ctx, path)
	return err
}

func (c *Client) RequestPullRequestChanges(ctx context.Context, workspace, repo string, id int) (*ReviewActionResult, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/request-changes", repoPath(workspace, repo), id)
	data, err := c.post(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var result ReviewActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse request changes response: %w", err)
	}

	return &result, nil
}

func (c *Client) ClearPullRequestChanges(ctx context.Context, workspace, repo string, id int) error {
	path := fmt.Sprintf("%s/pullrequests/%d/request-changes", repoPath(workspace, repo), id)
	_, err := c.delete(ctx, path)
	return err
}

func (c *Client) GetPullRequestDiff(ctx context.Context, workspace, repo string, id int) ([]byte, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/diff", repoPath(workspace, repo), id)
	return c.getRaw(ctx, path)
}

// ListPullRequestTasks returns up to limit tasks (zero means MaxListItems);
// the Scan says whether there are more. A repository without tasks has none.
func (c *Client) ListPullRequestTasks(ctx context.Context, workspace, repo string, id, limit int) ([]Task, Scan, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/tasks", repoPath(workspace, repo), id)
	tasks, scan, err := collectCachedPages[Task](ctx, c, path, limit, "tasks")
	if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 404 {
		return []Task{}, Scan{}, nil
	}
	return tasks, scan, err
}

// extractNextPath returns the path of a next link under the base URL, or ""
// to stop paging when the link is missing or points elsewhere.
func (c *Client) extractNextPath(nextURL string) string {
	if nextURL == "" {
		return ""
	}
	path, err := c.apiPath(nextURL)
	if err != nil {
		return ""
	}
	return path
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
	// Limit caps the snippets returned. Zero means MaxListItems.
	Limit int
	Role  string
	// Private, when set, keeps only private (true) or public (false)
	// snippets. Bitbucket cannot filter by visibility, so this runs
	// client-side over at most MaxScan snippets (zero means MaxListItems).
	Private *bool
	MaxScan int
}

func snippetListPath(workspace string, opts *SnippetListOptions) string {
	values := url.Values{}
	if opts != nil {
		if opts.Private != nil {
			values.Set("pagelen", strconv.Itoa(maxPageLen))
		} else if opts.Limit > 0 {
			values.Set("pagelen", strconv.Itoa(min(opts.Limit, maxPageLen)))
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

// ListSnippets stops paging once it has Limit snippets, or has read MaxScan
// while filtering by visibility, and says which in the Scan.
func (c *Client) ListSnippets(ctx context.Context, workspace string, opts *SnippetListOptions) ([]Snippet, Scan, error) {
	limit, maxScan := 0, 0
	var keep func(Snippet) bool
	if opts != nil {
		limit, maxScan = opts.Limit, opts.MaxScan
		if private := opts.Private; private != nil {
			keep = func(snippet Snippet) bool { return snippet.IsPrivate == *private }
		}
	}
	return scanPages(ctx, pageFetcher{c.getCachedJSON, c.apiPath}, snippetListPath(workspace, opts), keep, limit, maxScan, "snippets")
}

func (c *Client) GetSnippet(ctx context.Context, workspace, id string) (*Snippet, error) {
	path := fmt.Sprintf("/snippets/%s/%s", url.PathEscape(workspace), url.PathEscape(id))
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var snippet Snippet
	if err := json.Unmarshal(data, &snippet); err != nil {
		return nil, fmt.Errorf("failed to parse snippet response: %w", err)
	}

	return &snippet, nil
}

func (c *Client) GetSnippetFileContent(ctx context.Context, workspace, id, filename string) ([]byte, error) {
	escapedFilename, err := escapePathPreservingSlashes(filename)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/snippets/%s/%s/files/%s", url.PathEscape(workspace), url.PathEscape(id), escapedFilename)
	return c.getRaw(ctx, path)
}

func (c *Client) CreateSnippet(ctx context.Context, workspace, title string, files map[string][]byte, isPrivate bool) (*Snippet, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}

	if err := c.checkResponse(resp, body); err != nil {
		return nil, err
	}

	var snippet Snippet
	if err := json.Unmarshal(body, &snippet); err != nil {
		return nil, fmt.Errorf("failed to parse snippet response: %w", err)
	}

	return &snippet, nil
}

func (c *Client) UpdateSnippet(ctx context.Context, workspace, id, title string, addFiles map[string][]byte, removeFiles []string) error {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return err
	}

	return c.checkResponse(resp, body)
}

func (c *Client) DeleteSnippet(ctx context.Context, workspace, id string) error {
	requestURL := c.baseURL + fmt.Sprintf("/snippets/%s/%s", url.PathEscape(workspace), url.PathEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, requestURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return err
	}

	return c.checkResponse(resp, body)
}

// PR reads include detail, filtered lists, and branch lookups. Clear all cached
// responses because the disk keys are hashes and cannot be matched by URL prefix.
// An invalidate-only cache is cleared too.
func (c *Client) invalidatePRCache() {
	if c.cache == nil {
		return
	}
	if err := c.cache.Clear(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: pull request saved, but failed to clear the response cache: %v; cached reads may be stale for %s\n", err, c.cache.ttl)
	}
}
