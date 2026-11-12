package repo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/repo"
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

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// transportEnv serves every Bitbucket request from respond, which returns
// the status and body.
func transportEnv(t *testing.T, respond func(*http.Request) (int, io.ReadCloser)) *opstest.Env {
	t.Helper()
	transport := roundTripper(func(req *http.Request) (*http.Response, error) {
		status, body := respond(req)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/plain"}}, Body: body, Request: req}, nil
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

func hugeEnv(t *testing.T, read *atomic.Int64) *opstest.Env {
	return transportEnv(t, func(*http.Request) (int, io.ReadCloser) {
		return http.StatusOK, &hugeBody{remaining: tenMiB, read: read}
	})
}

func TestFileReadNeverReadsPastTheHardMaximum(t *testing.T) {
	for _, tc := range []struct {
		maxBytes int
		want     int
	}{
		{0, 64 << 10},
		{repo.HardMaxFileBytes, repo.HardMaxFileBytes},
	} {
		var read atomic.Int64
		res, err := repo.FileRead.Invoke(context.Background(), hugeEnv(t, &read),
			json.RawMessage(fmt.Sprintf(`{"repo":"widgets","path":"big.txt","max_bytes":%d}`, tc.maxBytes)))
		if err != nil {
			t.Fatal(err)
		}
		if got := read.Load(); got > int64(tc.want)+1 {
			t.Fatalf("max_bytes %d: read %d bytes of a 10 MiB file, want at most %d", tc.maxBytes, got, tc.want+1)
		}
		entry := res.Value.(*repo.FileReadResult).Results[0]
		if !entry.Truncated || entry.Bytes != tc.want {
			t.Fatalf("max_bytes %d: entry has %d bytes, truncated=%v", tc.maxBytes, entry.Bytes, entry.Truncated)
		}
	}
	for _, args := range []string{
		`{"repo":"widgets","path":"big.txt","max_bytes":-1}`,
		`{"repo":"widgets","path":"big.txt","max_bytes":` + strconv.Itoa(repo.HardMaxFileBytes+1) + `}`,
		`{"repos":["widgets"],"path":"big.txt","max_bytes":-1}`,
		`{"repos":["widgets"],"path":"big.txt","max_bytes":` + strconv.Itoa(repo.HardMaxBatchFileBytes+1) + `}`,
	} {
		var read atomic.Int64
		if _, err := repo.FileRead.Invoke(context.Background(), hugeEnv(t, &read), json.RawMessage(args)); ops.KindOf(err) != ops.Invalid || read.Load() != 0 {
			t.Errorf("%s: err = %v, read %d bytes; want Invalid before any read", args, err, read.Load())
		}
	}
}

func TestFileReadBatchStaysWithinItsBudget(t *testing.T) {
	repos := make([]string, 20)
	for i := range repos {
		repos[i] = fmt.Sprintf("acme/r%02d", i)
	}
	var read atomic.Int64
	args, _ := json.Marshal(map[string]any{"repos": repos, "path": "big.txt", "max_bytes": repo.HardMaxBatchFileBytes})
	res, err := repo.FileRead.Invoke(context.Background(), hugeEnv(t, &read), args)
	if err != nil {
		t.Fatal(err)
	}
	const budget = 8 << 20
	share := budget / len(repos)
	if got := read.Load(); got > budget+int64(len(repos)) {
		t.Fatalf("read %d bytes across %d 10 MiB files, want at most the %d-byte budget", got, len(repos), budget)
	}
	for _, entry := range res.Value.(*repo.FileReadResult).Results {
		if entry.Status != repo.FileOK || !entry.Truncated || entry.Bytes != share {
			t.Fatalf("entry = %s %s, %d bytes, truncated=%v; want %d bytes, truncated", entry.Repo, entry.Status, entry.Bytes, entry.Truncated, share)
		}
	}
	if !strings.Contains(res.Text, fmt.Sprintf("Truncated at %d bytes, this repository's share of the %d-byte batch budget", share, budget)) {
		t.Fatalf("markdown does not explain the budget:\n%s", res.Text[len(res.Text)-300:])
	}
}

func TestFileReadKeepsPathBytes(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	env := transportEnv(t, func(req *http.Request) (int, io.ReadCloser) {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, req.URL.EscapedPath())
		return http.StatusOK, io.NopCloser(strings.NewReader("x"))
	})
	for path, want := range map[string]string{
		" leading.txt":   "/2.0/repositories/acme/widgets/src/HEAD/%20leading.txt",
		"trailing.txt ":  "/2.0/repositories/acme/widgets/src/HEAD/trailing.txt%20",
		"dir/ in dir /x": "/2.0/repositories/acme/widgets/src/HEAD/dir/%20in%20dir%20/x",
	} {
		paths = nil
		args, _ := json.Marshal(map[string]any{"repo": "widgets", "path": path})
		if _, err := repo.FileRead.Invoke(context.Background(), env, args); err != nil {
			t.Fatalf("path %q: %v", path, err)
		}
		if len(paths) != 1 || paths[0] != want {
			t.Errorf("path %q requested %v, want %s", path, paths, want)
		}
	}
	for _, path := range []string{"", "/version.json", "a//b", "a/", "./a", "a/./b", "../a", "a/..", "a\x00b", "a\nb", "a\x7fb"} {
		paths = nil
		args, _ := json.Marshal(map[string]any{"repo": "widgets", "path": path})
		if _, err := repo.FileRead.Invoke(context.Background(), env, args); ops.KindOf(err) != ops.Invalid || len(paths) != 0 {
			t.Errorf("path %q: err = %v, requests %v; want Invalid before any request", path, err, paths)
		}
	}
}

func TestFileReadBinaryDetection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  string
		maxBytes int
		binary   bool
	}{
		{"text", "abc日本", 0, false},
		{"cut mid-rune", "abc日本", 5, false},
		{"cut after three bytes of a four-byte rune", "ab😀", 5, false},
		{"invalid bytes at the cap", "abc\xff\xff\xff\xff", 6, true},
		{"invalid byte then the cap", "abc\xff\xe6\x97\xa5", 5, true},
		{"lead byte with a bad continuation at the cap", "abc\xe6\x41zz", 5, true},
		{"stray continuation bytes at the cap", "abc\x97\xa5xyz", 5, true},
		{"invalid UTF-8, not truncated", "abc\xff", 0, true},
		{"incomplete rune, not truncated", "abc\xe6\x97", 0, true},
		{"NUL", "a\x00b", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := transportEnv(t, func(*http.Request) (int, io.ReadCloser) {
				return http.StatusOK, io.NopCloser(strings.NewReader(tc.content))
			})
			args, _ := json.Marshal(map[string]any{"repo": "widgets", "path": "f", "max_bytes": tc.maxBytes})
			res, err := repo.FileRead.Invoke(context.Background(), env, args)
			if err != nil {
				t.Fatal(err)
			}
			if entry := res.Value.(*repo.FileReadResult).Results[0]; entry.Binary != tc.binary {
				t.Fatalf("%q (max_bytes %d): binary = %v, want %v", tc.content, tc.maxBytes, entry.Binary, tc.binary)
			}
		})
	}
}
