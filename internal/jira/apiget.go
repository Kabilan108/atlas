package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// LimitedResponse is a response body read up to a byte limit.
type LimitedResponse struct {
	Status      int
	ContentType string
	Body        []byte
	Truncated   bool
}

// GetAPI GETs a gateway path that APIPathAllowed accepts and reads at most
// maxBytes of the body. It is the read-only passthrough behind the api op.
func (c *Client) GetAPI(ctx context.Context, path string, maxBytes int64) (*LimitedResponse, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("byte limit %d must be positive", maxBytes)
	}
	if !APIPathAllowed(path) {
		return nil, fmt.Errorf("jira: refusing API path %q", path)
	}
	rawPath, rawQuery, _ := cutQuery(path)
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("jira: invalid query in %q: %w", path, err)
	}
	req, err := c.newRequest(ctx, http.MethodGet, rawPath, query, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newAPIError(req, resp, data, c.redact)
	}
	out := &LimitedResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: data}
	if int64(len(data)) > maxBytes {
		out.Body, out.Truncated = data[:maxBytes], true
	}
	return out, nil
}

func cutQuery(path string) (string, string, bool) {
	for i := 0; i < len(path); i++ {
		if path[i] == '?' {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}
