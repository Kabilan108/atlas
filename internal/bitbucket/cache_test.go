package bitbucket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		user, _, _ := r.BasicAuth()
		writeJSON(w, http.StatusOK, `{"account_id":"`+user+`"}`)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// The MCP server builds clients without WithCache, possibly with no HOME, and
// must never read or write a cache directory.
func TestClientWithoutCacheNeverTouchesDisk(t *testing.T) {
	cacheHome := t.TempDir()
	tests := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"both unset", func(t *testing.T) {
			t.Setenv("HOME", "")
			t.Setenv("XDG_CACHE_HOME", "")
			os.Unsetenv("HOME")
			os.Unsetenv("XDG_CACHE_HOME")
		}},
		{"both set", func(t *testing.T) {
			t.Setenv("HOME", cacheHome)
			t.Setenv("XDG_CACHE_HOME", cacheHome)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			server, hits := countingServer(t)
			client, err := NewClientWithCredentials("user", "token", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := client.GetCurrentUser(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := client.DeclinePullRequest(context.Background(), "ws", "repo", 1); err != nil {
				t.Fatal(err)
			}
			if got := hits.Load(); got != 3 {
				t.Fatalf("server hits = %d, want 3 (no cached reads)", got)
			}
			entries, err := os.ReadDir(cacheHome)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("client wrote %d entries under the cache home", len(entries))
			}
		})
	}
}

func TestCacheKeysIncludeUsername(t *testing.T) {
	server, hits := countingServer(t)
	cache := NewCache(t.TempDir())
	newClient := func(user string) *Client {
		client, err := NewClientWithCredentials(user, "token", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithCache(cache))
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	for _, user := range []string{"alice", "bob", "alice"} {
		got, err := newClient(user).GetCurrentUser(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.AccountID != user {
			t.Fatalf("%s read %s's cached response", user, got.AccountID)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want 2 (alice's second read cached)", got)
	}
}

func TestInvalidateOnlyCacheSkipsReadsButClearsAfterWrites(t *testing.T) {
	server, hits := countingServer(t)
	cache := NewCache(t.TempDir())
	cached, err := NewClientWithCredentials("user", "token", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	bypass, err := NewClientWithCredentials("user", "token", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithCache(cache.InvalidateOnly()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := cached.GetCurrentUser(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bypass.GetCurrentUser(ctx); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want the bypassing read to skip the cache", got)
	}
	if _, err := bypass.UpdatePullRequest(ctx, "ws", "repo", 1, PullRequestUpdate{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.GetCurrentUser(ctx); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 4 {
		t.Fatalf("server hits = %d, want the write to clear the cached read", got)
	}
}
