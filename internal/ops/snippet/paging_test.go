package snippet_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/snippet"
)

// pagedSnippets serves total snippets page by page, all private or all
// public, and counts the requests.
func pagedSnippets(t *testing.T, total int, private bool, requests *atomic.Int64) *opstest.Env {
	t.Helper()
	transport := roundTripper(func(r *http.Request) (*http.Response, error) {
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
			values = append(values, `{"id":"s`+strconv.Itoa(i)+`","title":"t","is_private":`+strconv.FormatBool(private)+`}`)
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
	})
	client, err := bitbucket.NewClientWithCredentials(opstest.Email, opstest.Token,
		bitbucket.WithBaseURL("https://api.bitbucket.org/2.0"), bitbucket.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	env := opstest.New("acme")
	env.BitbucketClient = client
	return env
}

func TestFilteredListStopsOnceItHasEnough(t *testing.T) {
	var requests atomic.Int64
	res, err := snippet.List.Invoke(context.Background(), pagedSnippets(t, 30000, true, &requests), json.RawMessage(`{"visibility":"private","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("%d requests although the first page had a match, want 1", got)
	}
	if !strings.Contains(res.Text, "Showing the first 1") {
		t.Fatalf("markdown does not say there are more:\n%s", res.Text)
	}
}

func TestFilteredListScanIsBounded(t *testing.T) {
	var requests atomic.Int64
	res, err := snippet.List.Invoke(context.Background(), pagedSnippets(t, 30000, false, &requests), json.RawMessage(`{"visibility":"private"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 10 {
		t.Fatalf("%d requests with no private snippets, want 10 (1000 snippets)", got)
	}
	if !strings.Contains(res.Text, "Checked only the first 1000 snippets") {
		t.Fatalf("scan cap not reported:\n%s", res.Text)
	}
}

func TestListLimitHasAHardMaximum(t *testing.T) {
	var requests atomic.Int64
	_, err := snippet.List.Invoke(context.Background(), pagedSnippets(t, 30000, true, &requests), json.RawMessage(`{"limit":1000000}`))
	if ops.KindOf(err) != ops.Invalid || requests.Load() != 0 {
		t.Fatalf("err = %v after %d requests, want Invalid before any request", err, requests.Load())
	}
}
