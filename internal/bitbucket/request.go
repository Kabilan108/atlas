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

	data, err := readBody(resp)
	if err != nil {
		return nil, nil, err
	}
	if err := c.checkResponseWithMessage(resp, data); err != nil {
		return resp, nil, err
	}
	return resp, data, nil
}

// getCachedJSON is getJSON through the response cache.
func (c *Client) getCachedJSON(ctx context.Context, path string, out any, what string) error {
	data, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", what, err)
	}
	return nil
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

// MaxListItems bounds every list the client collects, whatever the caller
// asks for, and maxListPages the pages one list reads however short the
// pages a server sends. Ops apply smaller maxima of their own; these are
// the backstop for any caller that asks for more, or for everything.
const (
	MaxListItems = 2000
	maxListPages = 100
)

// Scan says where a bounded walk over a paginated list stopped.
type Scan struct {
	// Scanned counts the values read, before any filter.
	Scanned int
	// More is true when values past those returned exist or may exist: a
	// match past the limit was read, or the walk stopped with a next page
	// unread.
	More bool
	// Capped is true when the walk stopped at its scan maximum or the page
	// maximum before collecting limit values, so matches it never read may
	// exist.
	Capped bool
}

// clampList maps a limit outside (0, MaxListItems] to MaxListItems.
func clampList(n int) int {
	if n <= 0 || n > MaxListItems {
		return MaxListItems
	}
	return n
}

// collectPages follows Bitbucket's next links until limit values are
// collected or the last page, reading at most maxListPages pages. A limit
// outside (0, MaxListItems] means MaxListItems.
func collectPages[T any](ctx context.Context, c *Client, path string, limit int, what string) ([]T, Scan, error) {
	return scanPages[T](ctx, c.pages(), path, nil, limit, 0, what)
}

// collectCachedPages is collectPages through the response cache, for the
// lists the CLI has always cached.
func collectCachedPages[T any](ctx context.Context, c *Client, path string, limit int, what string) ([]T, Scan, error) {
	return scanPages[T](ctx, pageFetcher{c.getCachedJSON, c.apiPath}, path, nil, limit, 0, what)
}

// pages fetches uncached pages.
func (c *Client) pages() pageFetcher {
	return pageFetcher{c.getJSON, c.apiPath}
}

// collectAll is collectPages for a list a caller needs whole, such as the
// statuses behind a merge decision: it fails rather than return part of a
// list longer than maximum.
func collectAll[T any](ctx context.Context, c *Client, path string, maximum int, what string) ([]T, error) {
	values, scan, err := collectPages[T](ctx, c, path, maximum, what)
	if err != nil {
		return nil, err
	}
	if scan.More {
		return nil, fmt.Errorf("more than %d %s; atlas reads at most %d", maximum, what, maximum)
	}
	return values, nil
}

// pageFetcher reads one page into out and resolves the next link, which
// must stay under the client's base URL.
type pageFetcher struct {
	get  func(ctx context.Context, path string, out any, what string) error
	next func(raw string) (string, error)
}

// scanPages is collectPages keeping only the values keep accepts (all of
// them when keep is nil) and reading at most maxScanned values, for filters
// Bitbucket cannot apply itself. maxScanned outside (0, MaxListItems] means
// MaxListItems.
func scanPages[T any](ctx context.Context, fetch pageFetcher, path string, keep func(T) bool, limit, maxScanned int, what string) ([]T, Scan, error) {
	limit, maxScanned = clampList(limit), clampList(maxScanned)
	var values []T
	var scan Scan
	visited := map[string]bool{}
	for pages := 0; path != ""; pages++ {
		if pages == maxListPages {
			scan.More, scan.Capped = true, len(values) < limit
			break
		}
		if visited[path] {
			return nil, scan, fmt.Errorf("%s pagination did not advance: %s", what, path)
		}
		visited[path] = true

		var page PaginatedResponse[T]
		if err := fetch.get(ctx, path, &page, what); err != nil {
			return nil, scan, err
		}
		for _, value := range page.Values {
			if scan.Scanned == maxScanned {
				scan.More, scan.Capped = true, len(values) < limit
				return values, scan, nil
			}
			scan.Scanned++
			if keep != nil && !keep(value) {
				continue
			}
			if len(values) == limit {
				scan.More = true
				return values, scan, nil
			}
			values = append(values, value)
		}
		if len(page.Values) == 0 || page.Next == "" {
			break
		}
		if len(values) == limit || scan.Scanned == maxScanned {
			scan.More, scan.Capped = true, len(values) < limit
			break
		}
		next, err := fetch.next(page.Next)
		if err != nil {
			return nil, scan, fmt.Errorf("%s next page: %w", what, err)
		}
		path = next
	}
	return values, scan, nil
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
