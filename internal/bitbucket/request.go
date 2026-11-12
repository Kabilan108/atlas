package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxPageLen = 100

// send performs an uncached JSON request. New endpoints use it instead of get
// because statuses, pipelines, and merge tasks change faster than the cache TTL.
func (c *Client) send(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode request body: %w", err)
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if err := c.checkResponseWithMessage(resp, data); err != nil {
		return resp, nil, err
	}
	return resp, data, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any, what string) error {
	_, data, err := c.send(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", what, err)
	}
	return nil
}

// collectPages follows Bitbucket's next links until the last page or until
// limit values are collected. A limit of zero or less collects every page.
func collectPages[T any](ctx context.Context, c *Client, path string, limit int, what string) ([]T, error) {
	var values []T
	visited := map[string]bool{}
	for path != "" {
		if visited[path] {
			return nil, fmt.Errorf("%s pagination did not advance: %s", what, path)
		}
		visited[path] = true

		var page PaginatedResponse[T]
		if err := c.getJSON(ctx, path, &page, what); err != nil {
			return nil, err
		}

		values = append(values, page.Values...)
		if limit > 0 && len(values) >= limit {
			return values[:limit], nil
		}
		if len(page.Values) == 0 || page.Next == "" {
			break
		}
		next, err := c.apiPath(page.Next)
		if err != nil {
			return nil, fmt.Errorf("%s next page: %w", what, err)
		}
		path = next
	}
	return values, nil
}

// apiPath turns a server-supplied URL, absolute or relative to the base URL,
// into a path to append to the base URL. Anything outside the base URL is
// refused because the result is requested with credentials.
func (c *Client) apiPath(raw string) (string, error) {
	base, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return "", err
	}
	target, err := base.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	basePath, ok := c.apiScopePath(target)
	if !ok {
		return "", fmt.Errorf("refusing URL outside %s: %s", c.baseURL, target.Redacted())
	}

	path := strings.TrimPrefix(target.EscapedPath(), basePath)
	if target.RawQuery != "" {
		path += "?" + target.RawQuery
	}
	return path, nil
}

// apiScopePath reports whether target has the base URL's scheme, host, and
// port, no user info, and a path at or below the base path, and returns the
// escaped base path. Dot segments are refused so servers cannot normalize the
// path back out of the base path.
func (c *Client) apiScopePath(target *url.URL) (string, bool) {
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Host == "" || target.User != nil || target.Opaque != "" {
		return "", false
	}
	if !strings.EqualFold(target.Scheme, base.Scheme) ||
		!strings.EqualFold(target.Hostname(), base.Hostname()) ||
		effectivePort(target) != effectivePort(base) {
		return "", false
	}

	basePath := strings.TrimSuffix(base.EscapedPath(), "/")
	targetPath := target.EscapedPath()
	if targetPath != basePath && !strings.HasPrefix(targetPath, basePath+"/") {
		return "", false
	}
	for _, segment := range strings.Split(target.Path, "/") {
		if segment == "." || segment == ".." {
			return "", false
		}
	}
	return basePath, true
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

func pageLenFor(limit, explicit int) int {
	pageLen := explicit
	if pageLen <= 0 {
		pageLen = limit
	}
	if pageLen > maxPageLen {
		pageLen = maxPageLen
	}
	return pageLen
}

// fullPageLen sizes pages for lists that may walk every page: the limit when
// set, otherwise the maximum to keep the request count down.
func fullPageLen(limit int) int {
	if pageLen := pageLenFor(limit, 0); pageLen > 0 {
		return pageLen
	}
	return maxPageLen
}

func withQuery(path string, values url.Values) string {
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

func repoPath(workspace, repo string) string {
	return fmt.Sprintf("/repositories/%s/%s", url.PathEscape(workspace), url.PathEscape(repo))
}

// bbqlString quotes a value for Bitbucket's query language (the q parameter).
func bbqlString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// escapeSegments escapes each slash-separated segment, keeping the slashes.
func escapeSegments(kind, value string) (string, error) {
	trimmed := strings.TrimPrefix(value, "/")
	if trimmed == "" {
		return "", fmt.Errorf("%s cannot be empty", kind)
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		if part == "" {
			return "", fmt.Errorf("%s %q cannot contain empty path segments", kind, value)
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("%s %q cannot contain dot segments", kind, value)
		}
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/"), nil
}
