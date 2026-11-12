package bitbucket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestUncachedReadsPastTheCacheAndKeepsItCleared(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7,"state":"OPEN"}`))
	}))
	t.Cleanup(server.Close)
	cache := NewCache(t.TempDir())
	cached, err := NewClientWithCredentials("user", "token", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []*Client{cached, cached, cached.Uncached(), cached.Uncached()} {
		if _, err := client.GetPullRequest(context.Background(), "ws", "repo", 7); err != nil {
			t.Fatal(err)
		}
	}
	if n := hits.Load(); n != 3 {
		t.Fatalf("hits = %d, want 1 cached read and 2 uncached", n)
	}
	if cached.Uncached().cache.serves() || !cached.cache.serves() {
		t.Fatal("Uncached must not change the original client's cache")
	}
	if (&Client{}).Uncached().cache != nil {
		t.Fatal("a client without a cache stays without one")
	}
}
