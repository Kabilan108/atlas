package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/api"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

const tenMiB = 10 << 20

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

func hugeEnv(t *testing.T, read *atomic.Int64) *opstest.Env {
	t.Helper()
	transport := roundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       &hugeBody{remaining: tenMiB, read: read},
			Request:    req,
		}, nil
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

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGetNeverReadsPastTheHardMaximum(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int
		wantRead int64
	}{
		{"default", 0, 32<<10 + 1},
		{"hard maximum", api.HardMaxBytes, api.HardMaxBytes + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var read atomic.Int64
			res, err := api.Get.Invoke(context.Background(), hugeEnv(t, &read),
				mustJSON(t, map[string]any{"path": "/2.0/repositories/acme/app/src/main/big.txt", "max_bytes": tc.maxBytes}))
			if err != nil {
				t.Fatal(err)
			}
			if got := read.Load(); got > tc.wantRead {
				t.Fatalf("read %d bytes of a 10 MiB body, want at most %d", got, tc.wantRead)
			}
			if result := res.Value.(*api.GetResult); !result.Truncated || int64(len(result.Body)) != tc.wantRead-1 {
				t.Fatalf("body %d bytes, truncated=%v", len(result.Body), result.Truncated)
			}
		})
	}
	for _, maxBytes := range []int{-1, -2, api.HardMaxBytes + 1} {
		var read atomic.Int64
		_, err := api.Get.Invoke(context.Background(), hugeEnv(t, &read),
			json.RawMessage(`{"path":"/2.0/user","max_bytes":`+strconv.Itoa(maxBytes)+`}`))
		if ops.KindOf(err) != ops.Invalid || read.Load() != 0 {
			t.Errorf("max_bytes %d: err = %v, read %d bytes; want Invalid before any read", maxBytes, err, read.Load())
		}
	}
}
