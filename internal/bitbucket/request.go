package bitbucket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxPageLen = 100

// send performs an uncached JSON request. New endpoints use it instead of get
// because statuses, pipelines, and merge tasks change faster than the cache TTL.
func (c *Client) send(method, path string, body any) (*http.Response, []byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode request body: %w", err)
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, payload)
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
	if err := checkResponseWithMessage(resp, data); err != nil {
		return resp, data, err
	}
	return resp, data, nil
}

func (c *Client) getJSON(path string, out any, what string) error {
	_, data, err := c.send(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", what, err)
	}
	return nil
}

// collectPages follows Bitbucket pagination until the last page or until limit
// values are collected. A limit of zero or less collects every page.
func collectPages[T any](c *Client, path string, limit int, what string) ([]T, error) {
	var values []T
	for path != "" {
		var page PaginatedResponse[T]
		if err := c.getJSON(path, &page, what); err != nil {
			return nil, err
		}

		values = append(values, page.Values...)
		if limit > 0 && len(values) >= limit {
			return values[:limit], nil
		}
		if len(page.Values) == 0 {
			break
		}
		path = c.nextPagePath(path, page.Next, page.Page, page.PageLen, page.Size)
	}
	return values, nil
}

// nextPagePath prefers the server's next link. Pipelines endpoints report only
// page, pagelen, and size, so fall back to requesting the following page number.
func (c *Client) nextPagePath(current, next string, page, pageLen, size int) string {
	if next != "" {
		return c.extractNextPath(next)
	}
	if page <= 0 || pageLen <= 0 || page*pageLen >= size {
		return ""
	}

	parsed, err := url.Parse(current)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	query.Set("page", strconv.Itoa(page+1))
	parsed.RawQuery = query.Encode()
	return parsed.String()
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

// checkResponseWithMessage keeps the auth, not-found, and rate-limit mapping of
// checkResponse but replaces the raw JSON body with Bitbucket's error message.
func checkResponseWithMessage(resp *http.Response, body []byte) error {
	err := checkResponse(resp, body)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusNotFound, http.StatusTooManyRequests:
		return err
	}
	if message := bitbucketErrorMessage(body); message != "" {
		apiErr.Message = message
	}
	return apiErr
}

func bitbucketErrorMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string          `json:"message"`
			Detail  json.RawMessage `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	message := strings.TrimSpace(payload.Error.Message)
	var detail string
	if len(payload.Error.Detail) > 0 && json.Unmarshal(payload.Error.Detail, &detail) == nil {
		detail = strings.TrimSpace(detail)
		if detail != "" && detail != message {
			if message == "" {
				return detail
			}
			return message + ": " + detail
		}
	}
	return message
}
