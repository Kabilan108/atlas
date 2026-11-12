package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/api"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

func TestGetRejectsPathsOutsideTheAPI(t *testing.T) {
	var served int
	env := opstest.New("acme").WithBitbucket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
	}))
	for _, path := range []string{
		"",
		"https://api.bitbucket.org/2.0/user",
		"//evil.example/2.0/user",
		"2.0/user",
		"/2.0/../user",
		"/2.0/repositories/%2e%2e/x",
		"/2.0/repositories/a/%2E%2E/b",
		`/2.0\user`,
		"/2.0/user#frag",
		"/2.0/user\n",
		"/1.0/user",
		"/rest/api/3/myself",
		"/2.0",
	} {
		_, err := api.Get.Invoke(context.Background(), env, mustJSON(t, map[string]any{"path": path}))
		if ops.KindOf(err) != ops.Invalid {
			t.Errorf("path %q: err = %v, want Invalid", path, err)
		}
	}
	if served != 0 {
		t.Fatalf("served %d requests for rejected paths", served)
	}
}

func TestGetFetchesTruncatesAndObserves(t *testing.T) {
	var got []string
	env := opstest.New("acme").WithBitbucket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"values":[{"name":"v1.0.0"},{"name":"v1.1.0"}]}`))
	}))
	var mu sync.Mutex
	var calls []api.Call
	ctx := api.WithObserver(context.Background(), func(call api.Call) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call)
	})

	res, err := api.Get.Invoke(ctx, env, json.RawMessage(`{"path":"/2.0/repositories/acme/app/refs/tags?pagelen=2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "GET /2.0/repositories/acme/app/refs/tags?pagelen=2" {
		t.Fatalf("requests = %v", got)
	}
	if !strings.HasPrefix(res.Text, "GET /2.0/repositories/acme/app/refs/tags?pagelen=2: 200\n\n```json\n{\n  \"values\"") {
		t.Fatalf("text = %q", res.Text)
	}

	res, err = api.Get.Invoke(ctx, env, json.RawMessage(`{"path":"/2.0/repositories/acme/app/refs/tags","max_bytes":10}`))
	if err != nil {
		t.Fatal(err)
	}
	result := res.Value.(*api.GetResult)
	if !result.Truncated || result.Body != `{"values":` || !strings.Contains(res.Text, "Truncated at 10 bytes") {
		t.Fatalf("truncated result = %+v\n%s", result, res.Text)
	}
	want := []api.Call{
		{Method: "GET", Template: "/2.0/repositories/{workspace}/{repo}/refs/tags"},
		{Method: "GET", Template: "/2.0/repositories/{workspace}/{repo}/refs/tags"},
	}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("observed %v, want %v", calls, want)
	}
}

func TestGetPassesErrorsThrough(t *testing.T) {
	env := opstest.New("acme").WithBitbucket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"type":"error","error":{"message":"nope"}}`, http.StatusNotFound)
	}))
	_, err := api.Get.Invoke(context.Background(), env, json.RawMessage(`{"path":"/2.0/repositories/acme/missing"}`))
	if ops.KindOf(err) != ops.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestTemplate(t *testing.T) {
	for path, want := range map[string]string{
		"/2.0/user": "/2.0/user",
		"/2.0/repositories/acme/app/pullrequests/42/comments?pagelen=50":                    "/2.0/repositories/{workspace}/{repo}/pullrequests/{id}/comments",
		"/2.0/repositories/acme/app/src/main/deploy/k8s.yaml":                               "/2.0/repositories/{workspace}/{repo}/src/{commit}/{path}",
		"/2.0/repositories/acme/app/refs/branches/feature/x":                                "/2.0/repositories/{workspace}/{repo}/refs/branches/{name}",
		"/2.0/repositories/acme/app/commit/0a1b2c3d4e5f/statuses":                           "/2.0/repositories/{workspace}/{repo}/commit/{commit}/statuses",
		"/2.0/repositories/acme/app/pipelines/{8f0e1c2d-1111-2222-3333-444455556666}/steps": "/2.0/repositories/{workspace}/{repo}/pipelines/{uuid}/steps",
		"/2.0/workspaces/acme/members":                                                      "/2.0/workspaces/{workspace}/members",
		"/2.0/repositories/acme":                                                            "/2.0/repositories/{workspace}",
		"/rest/api/3/issue/CSM-756/comment":                                                 "/rest/api/3/issue/{issue}/comment",
	} {
		if got := api.Template(path); got != want {
			t.Errorf("Template(%q) = %q, want %q", path, got, want)
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
