package pr_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

type countingBody struct {
	remaining int64
	read      *atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), b.remaining)
	for i := range n {
		p[i] = '+'
	}
	b.remaining -= n
	b.read.Add(n)
	return int(n), nil
}

func (b *countingBody) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// pr_view with comments reads the diff only for inline-comment context, so a
// huge diff must be cut at pr_diff's download cap rather than read whole.
func TestViewReadsAtMostTheDiffDownloadCap(t *testing.T) {
	fixture := prtest.New()
	var read atomic.Int64
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/diff") {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}},
				Body: &countingBody{remaining: 64 << 20, read: &read}, Request: r}, nil
		}
		return fixture.RoundTrip(r)
	})
	client, err := bitbucket.NewClientWithCredentials(opstest.Email, opstest.Token,
		bitbucket.WithBaseURL("https://api.bitbucket.org/2.0"), bitbucket.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	env := opstest.New("acme")
	env.BitbucketClient = client
	if _, err := pr.View.Call(context.Background(), env, pr.ViewInput{Ref: pr.Ref{RepoRef: ops.RepoRef{Repo: "acme/widgets"}, PR: "42"}, Comments: true}); err != nil {
		t.Fatal(err)
	}
	if got := read.Load(); got == 0 || got > 16<<20+1 {
		t.Fatalf("pr_view read %d bytes of a 64 MiB diff, want at most %d", got, 16<<20+1)
	}
}
