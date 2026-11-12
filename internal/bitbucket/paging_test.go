package bitbucket

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// pagedTransport serves a list of total values at every path, page by page
// as Bitbucket does (pagelen defaults to 10), and counts the requests.
func pagedTransport(total int, item func(i int) string, requests *atomic.Int64) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		page = max(page, 1)
		size, _ := strconv.Atoi(query.Get("pagelen"))
		if size < 1 {
			size = 10
		}
		start := (page - 1) * size
		var values []string
		for i := start; i < min(start+size, total); i++ {
			values = append(values, item(i))
		}
		body := `{"values":[` + strings.Join(values, ",") + `]`
		if start+size < total {
			next := *r.URL
			query.Set("page", strconv.Itoa(page+1))
			next.RawQuery = query.Encode()
			body += `,"next":"` + next.String() + `"`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body + "}")), Request: r}, nil
	}
}

// Every list stops paging at a finite ceiling, whatever limit the caller
// passes, including zero ("all") and huge values. Lists a caller needs
// whole fail instead of returning part.
func TestListsNeverFetchAHugeSetWhole(t *testing.T) {
	const total = 30000
	item := func(i int) string {
		return `{"name":"item-` + strconv.Itoa(i) + `","uuid":"{` + strconv.Itoa(i) + `}","build_number":` + strconv.Itoa(i+1) + `}`
	}
	ctx := context.Background()
	calls := map[string]struct {
		run      func(*Client) (int, error)
		wantErr  bool
		maxItems int
	}{
		"repositories": {run: func(c *Client) (int, error) {
			v, err := c.SearchRepositories(ctx, "ws", &RepositoryListOptions{Limit: 1 << 30})
			return len(v), err
		}, maxItems: 2000},
		"branches": {run: func(c *Client) (int, error) {
			v, err := c.ListBranches(ctx, "ws", "repo", &BranchListOptions{})
			return len(v), err
		}, maxItems: 2000},
		"commits": {run: func(c *Client) (int, error) {
			v, err := c.ListCommits(ctx, "ws", "repo", "main", &CommitListOptions{Limit: 1 << 30})
			return len(v), err
		}, maxItems: 2000},
		"activity": {run: func(c *Client) (int, error) {
			v, err := c.ListPullRequestActivity(ctx, "ws", "repo", 7, 0)
			return len(v), err
		}, maxItems: 2000},
		"pipelines": {run: func(c *Client) (int, error) {
			v, err := c.ListPipelines(ctx, "ws", "repo", &PipelineListOptions{Limit: 1 << 30})
			return len(v), err
		}, maxItems: 2000},
		"pr statuses": {run: func(c *Client) (int, error) {
			v, err := c.ListPullRequestStatuses(ctx, "ws", "repo", 7)
			return len(v), err
		}, wantErr: true},
		"commit statuses": {run: func(c *Client) (int, error) {
			v, err := c.ListCommitStatuses(ctx, "ws", "repo", "abc123")
			return len(v), err
		}, wantErr: true},
		"pipeline steps": {run: func(c *Client) (int, error) {
			v, err := c.ListPipelineSteps(ctx, "ws", "repo", "{0f0e0d0c-0000-1111-2222-333344445555}")
			return len(v), err
		}, wantErr: true},
	}
	for name, call := range calls {
		var requests atomic.Int64
		client, err := NewClientWithCredentials("ada@example.invalid", "a-long-api-token",
			WithBaseURL("https://api.bitbucket.org/2.0"), WithHTTPClient(&http.Client{Transport: pagedTransport(total, item, &requests)}))
		if err != nil {
			t.Fatal(err)
		}
		n, err := call.run(client)
		if call.wantErr != (err != nil) {
			t.Errorf("%s: err = %v, want error %v", name, err, call.wantErr)
		}
		if got := requests.Load(); got > 100 {
			t.Errorf("%s: made %d requests for a %d-item list, want at most 100", name, got, total)
		}
		if !call.wantErr && (n == 0 || n > call.maxItems) {
			t.Errorf("%s: returned %d items, want 1..%d", name, n, call.maxItems)
		}
	}
}
