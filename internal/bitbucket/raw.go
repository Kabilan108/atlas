package bitbucket

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// LimitedBody is a response body read up to a byte limit.
type LimitedBody struct {
	Data []byte
	// Truncated is true when the body had more than the limit.
	Truncated   bool
	ContentType string
	StatusCode  int
}

// GetFileContentLimit is GetFileContent reading at most maxBytes of the
// file. Ref "HEAD" names the main branch.
func (c *Client) GetFileContentLimit(ctx context.Context, workspace, repo, ref, path string, maxBytes int64) (*LimitedBody, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("ref cannot be empty")
	}
	escapedPath, err := escapeSegments("file path", path)
	if err != nil {
		return nil, err
	}
	revision := ref
	if strings.Contains(ref, "/") {
		if revision, err = c.resolveSlashRef(ctx, workspace, repo, ref); err != nil {
			return nil, err
		}
	}
	body, err := c.getLimited(ctx, repoPath(workspace, repo)+"/src/"+url.PathEscape(revision)+"/"+escapedPath, maxBytes, false)
	if err != nil {
		if body != nil && body.StatusCode == http.StatusNotFound {
			return nil, NewNotFoundError("file", ref+":"+path)
		}
		return nil, err
	}
	return body, nil
}

// GetSnippetFileContentLimit is GetSnippetFileContent reading at most
// maxBytes of the file.
func (c *Client) GetSnippetFileContentLimit(ctx context.Context, workspace, id, filename string, maxBytes int64) (*LimitedBody, error) {
	escapedFilename, err := escapePathPreservingSlashes(filename)
	if err != nil {
		return nil, err
	}
	path := "/snippets/" + url.PathEscape(workspace) + "/" + url.PathEscape(id) + "/files/" + escapedFilename
	return c.getLimited(ctx, path, maxBytes, false)
}

// GetAPI sends an uncached GET for path (relative to the base URL, with an
// optional query) and reads at most maxBytes of the body. Callers validate
// path; the client still refuses one that resolves outside the base URL.
func (c *Client) GetAPI(ctx context.Context, path string, maxBytes int64) (*LimitedBody, error) {
	return c.getLimited(ctx, path, maxBytes, true)
}

func (c *Client) getLimited(ctx context.Context, path string, maxBytes int64, acceptJSON bool) (*LimitedBody, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("byte limit %d is negative", maxBytes)
	}
	requestURL := c.baseURL + path
	target, err := url.Parse(requestURL)
	if err != nil {
		return nil, fmt.Errorf("invalid API path %q: %w", path, err)
	}
	if _, ok := c.apiScopePath(target); !ok {
		return nil, fmt.Errorf("refusing path outside %s: %s", c.baseURL, path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	if acceptJSON {
		resp, err = c.do(req)
	} else {
		resp, err = c.doAuthenticated(req)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body := &LimitedBody{ContentType: resp.Header.Get("Content-Type"), StatusCode: resp.StatusCode}
	if !success(resp.StatusCode) {
		data, err := readBody(resp)
		if err != nil {
			return nil, err
		}
		return body, c.checkResponseWithMessage(resp, data)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	body.Data = data
	if int64(len(data)) > maxBytes {
		body.Data, body.Truncated = data[:maxBytes], true
	}
	return body, nil
}

// GetPullRequestDiffLimit is GetPullRequestDiff reading at most maxBytes of
// the diff.
func (c *Client) GetPullRequestDiffLimit(ctx context.Context, workspace, repo string, id int, maxBytes int64) (*LimitedBody, error) {
	path := fmt.Sprintf("%s/pullrequests/%d/diff", repoPath(workspace, repo), id)
	return c.getLimited(ctx, path, maxBytes, false)
}
