package bitbucket

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

const sixtyFourMiB = 64 << 20

// endlessBody serves remaining bytes of JSON-ish filler without holding
// them, counting every byte the client reads.
type endlessBody struct {
	remaining int64
	read      *atomic.Int64
}

func (b *endlessBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), b.remaining)
	for i := range n {
		p[i] = ' '
	}
	b.remaining -= n
	b.read.Add(n)
	return int(n), nil
}

func (b *endlessBody) Close() error { return nil }

func TestResponseReadsStopAtTheCeiling(t *testing.T) {
	for _, tc := range []struct {
		status  int
		maxRead int64
	}{
		{http.StatusOK, 32<<20 + 1},
		{http.StatusInternalServerError, 64 << 10},
	} {
		for call, run := range clientCalls {
			var read atomic.Int64
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: &endlessBody{remaining: sixtyFourMiB, read: &read}, Request: r}, nil
			})
			client, err := NewClientWithCredentials("ada@example.invalid", "a-long-api-token",
				WithBaseURL("https://api.bitbucket.org/2.0"), WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			if err := run(client); err == nil && call != "getLimited" {
				t.Errorf("%s %d: a 64 MiB response was accepted", call, tc.status)
			}
			if got := read.Load(); got > tc.maxRead {
				t.Errorf("%s %d: read %d bytes of a 64 MiB response, want at most %d", call, tc.status, got, tc.maxRead)
			}
		}
	}
}
