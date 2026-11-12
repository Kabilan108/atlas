// Package jira is a REST client for Jira Cloud and Jira Service Management,
// reached through the Atlassian API gateway with an email and API token.
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	gatewayBaseURL   = "https://api.atlassian.com/ex/jira/"
	defaultUserAgent = "atlas"
	defaultPageSize  = 100
	maxErrorBody     = 4096
	// maxResponseBytes bounds every response body the client reads; no
	// single Jira response atlas asks for comes close.
	maxResponseBytes = 32 << 20
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	email      string
	token      string
	userAgent  string
	// secrets are the credential encodings scrubbed from error content.
	secrets []string
}

type Option func(*Client)

// WithBaseURL replaces the gateway URL derived from the cloud ID, for tests or
// a direct site URL such as https://example.atlassian.net.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if userAgent != "" {
			c.userAgent = userAgent
		}
	}
}

// NewClientWithCredentials builds a client for
// https://api.atlassian.com/ex/jira/{cloudID}. cloudID may be empty only when
// WithBaseURL supplies the base URL.
func NewClientWithCredentials(email, token, cloudID string, opts ...Option) (*Client, error) {
	if email == "" || token == "" {
		return nil, errors.New("jira: missing email or API token")
	}
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		email:      email,
		token:      token,
		userAgent:  defaultUserAgent,
	}
	if cloudID != "" {
		c.baseURL = gatewayBaseURL + url.PathEscape(cloudID)
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.baseURL == "" {
		return nil, errors.New("jira: missing cloud ID")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("jira: invalid base URL %q", c.baseURL)
	}
	c.httpClient = sameOriginRedirects(c.httpClient, origin(base))
	c.secrets = credentialSecrets(email, token)
	return c, nil
}

// sameOriginRedirects returns a copy of hc that refuses any redirect leaving
// the base origin, so the Authorization header is never sent elsewhere. Go
// forwards it to subdomains, other ports and plain HTTP by default.
func sameOriginRedirects(hc *http.Client, baseOrigin string) *http.Client {
	clone := *hc
	next := hc.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if got := origin(req.URL); got != baseOrigin {
			return fmt.Errorf("jira: refusing redirect from %s to %s", baseOrigin, got)
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("jira: stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

func credentialSecrets(email, token string) []string {
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	secrets := []string{basic, strings.TrimRight(basic, "="), token}
	if escaped := url.QueryEscape(token); escaped != token {
		secrets = append(secrets, escaped)
	}
	return secrets
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	return req, nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newAPIError(req, resp, data, c.redact)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("jira: %s %s: response is larger than %d bytes", req.Method, req.URL.Path, maxResponseBytes)
	}
	return data, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	data, err := c.do(req)
	if err != nil {
		return err
	}
	return decode(data, out, path)
}

// sendJSON sends body (when non-nil) as JSON and decodes the response into out
// (when non-nil and the response is not empty).
func (c *Client) sendJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("jira: encode request body: %w", err)
		}
		payload = bytes.NewReader(data)
	}
	req, err := c.newRequest(ctx, method, path, query, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	data, err := c.do(req)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return decode(data, out, path)
}

func decode(data []byte, out any, path string) error {
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("jira: parse response from %s: %w", path, err)
	}
	return nil
}

func escape(segment string) string {
	return url.PathEscape(segment)
}

func issuePath(issueKey string, rest ...string) string {
	return strings.Join(append([]string{"/rest/api/3/issue", escape(issueKey)}, rest...), "/")
}

// pageInfo is the offset pagination state Jira returns alongside a page.
// IsLast is nil when the endpoint does not report it.
type pageInfo struct {
	StartAt    int
	MaxResults int
	Total      int
	IsLast     *bool
}

// collectOffset pages through an offset-paginated endpoint. limit <= 0 means
// every item.
func collectOffset[T any](limit, pageSize int, fetch func(startAt, maxResults int) ([]T, pageInfo, error)) ([]T, error) {
	var all []T
	startAt := 0
	for {
		size := pageSize
		if limit > 0 && limit-len(all) < size {
			size = limit - len(all)
		}
		items, info, err := fetch(startAt, size)
		if err != nil {
			return all, err
		}
		all = append(all, items...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		if len(items) == 0 {
			return all, nil
		}
		next := info.StartAt + len(items)
		switch {
		case info.IsLast != nil:
			if *info.IsLast {
				return all, nil
			}
		case next >= info.Total:
			return all, nil
		}
		startAt = next
	}
}

func pageQuery(startAt, maxResults int) url.Values {
	q := url.Values{}
	q.Set("startAt", fmt.Sprint(startAt))
	q.Set("maxResults", fmt.Sprint(maxResults))
	return q
}

func commaJoin(values []string) string {
	return strings.Join(values, ",")
}
