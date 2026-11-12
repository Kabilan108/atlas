package snippet_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/snippet"
	"github.com/kabilan108/atlas/internal/ops/snippet/snippettest"
)

const (
	tenMiB       = 10 << 20
	maxFileBytes = 4 << 20
)

// hugeBody serves size bytes without holding them, counting every byte read.
type hugeBody struct {
	remaining int64
	read      *atomic.Int64
}

func (b *hugeBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), b.remaining)
	for i := range n {
		p[i] = 'a'
	}
	b.remaining -= n
	b.read.Add(n)
	return int(n), nil
}

func (b *hugeBody) Close() error { return nil }

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// hugeFilesEnv serves the snippet fixture, except that every file GET
// returns 10 MiB.
func hugeFilesEnv(t *testing.T, read *atomic.Int64) (*opstest.Env, *snippettest.Fixture) {
	t.Helper()
	fixture := snippettest.New()
	transport := roundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/files/") {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}},
				Body: &hugeBody{remaining: tenMiB, read: read}, Request: req}, nil
		}
		return fixture.RoundTrip(req)
	})
	client, err := bitbucket.NewClientWithCredentials(opstest.Email, opstest.Token,
		bitbucket.WithBaseURL("https://"+snippettest.Host+"/2.0"), bitbucket.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	env := opstest.New(snippettest.Workspace)
	env.BitbucketClient = client
	return env, fixture
}

func TestViewNeverReadsAWholeHugeFile(t *testing.T) {
	var read atomic.Int64
	env, _ := hugeFilesEnv(t, &read)
	res, err := snippet.View.Invoke(context.Background(), env, json.RawMessage(`{"snippet":"abc123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, limit := read.Load(), int64(2*(maxFileBytes+1)); got > limit {
		t.Fatalf("read %d bytes for two 10 MiB files, want at most %d", got, limit)
	}
	result := res.Value.(*snippet.ViewResult)
	if want := []string{"notes.md", "run.sh"}; strings.Join(result.TruncatedFiles, ",") != strings.Join(want, ",") || result.FileBytes != maxFileBytes {
		t.Fatalf("truncated %v at %d, want %v at %d", result.TruncatedFiles, result.FileBytes, want, maxFileBytes)
	}
	for name, content := range result.FileContents {
		if len(content) != maxFileBytes {
			t.Fatalf("%s has %d bytes, want %d", name, len(content), maxFileBytes)
		}
	}
	if !strings.Contains(res.Text, "Truncated at 4194304 bytes.") {
		t.Fatal("markdown does not say the files were truncated")
	}
}

func TestRenameRefusesAFileOverTheCap(t *testing.T) {
	var read atomic.Int64
	env, fixture := hugeFilesEnv(t, &read)
	_, err := snippet.Rename.Invoke(context.Background(), env, json.RawMessage(`{"snippet":"abc123","from":"run.sh","to":"deploy.sh"}`))
	if ops.KindOf(err) != ops.Invalid {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if got := read.Load(); got > maxFileBytes+1 {
		t.Fatalf("read %d bytes of a 10 MiB file, want at most %d", got, maxFileBytes+1)
	}
	for _, req := range fixture.Requests() {
		if req.Method != http.MethodGet {
			t.Fatalf("sent %s after refusing; a truncated rename would lose data", req)
		}
	}
}
