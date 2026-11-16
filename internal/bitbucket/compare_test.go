package bitbucket

import (
	"context"
	"net/http"
	"testing"
)

func TestCompareSpecPutsHeadFirstAndResolvesSlashRefs(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/repositories/ws/repo/refs/branches/release/2.0":
			writeJSON(w, http.StatusOK, `{"name":"release/2.0","target":{"hash":"b4s3"}}`)
		default:
			writeJSON(w, http.StatusNotFound, `{"type":"error","error":{"message":"not found"}}`)
		}
	})

	tests := []struct {
		base, head, want string
		lookups          int
	}{
		{"main", "topic", "topic..main", 0},
		{"release/2.0", "topic", "topic..b4s3", 1},
		{"main", "v1.0+rc", "v1.0+rc..main", 0},
	}
	lookups := 0
	for _, tt := range tests {
		spec, err := client.CompareSpec(context.Background(), "ws", "repo", tt.base, tt.head)
		if err != nil {
			t.Fatalf("CompareSpec(%s, %s) error = %v", tt.base, tt.head, err)
		}
		if spec != tt.want {
			t.Errorf("CompareSpec(%s, %s) = %q, want %q", tt.base, tt.head, spec, tt.want)
		}
		lookups += tt.lookups
	}
	if got := len(recorder.all()); got != lookups {
		t.Fatalf("made %d requests, want %d branch lookups", got, lookups)
	}
}
