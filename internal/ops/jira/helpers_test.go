package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

func newEnv(t *testing.T, fake *jiratest.Fake) *opstest.Env {
	t.Helper()
	return opstest.New("").WithJira(t, fake)
}

// invoke runs def through its MCP path (JSON arguments in, text out).
func invoke(t *testing.T, fake *jiratest.Fake, def ops.Def, args string) (ops.Result, error) {
	t.Helper()
	return def.Invoke(context.Background(), newEnv(t, fake), json.RawMessage(args))
}

func mustInvoke(t *testing.T, fake *jiratest.Fake, def ops.Def, args string) ops.Result {
	t.Helper()
	res, err := invoke(t, fake, def, args)
	if err != nil {
		t.Fatalf("%s(%s): %v\nrequests: %v", def.Info().Name, args, err, fake.Routes())
	}
	return res
}

func wantKind(t *testing.T, err error, kind ops.Kind, contains ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %s", kind)
	}
	if got := ops.KindOf(err); got != kind {
		t.Errorf("kind = %s, want %s (%v)", got, kind, err)
	}
	for _, s := range contains {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not contain %q", err, s)
		}
	}
}

func wantContains(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("missing %q in:\n%s", part, text)
		}
	}
}

// body decodes a recorded JSON request body.
func body(t *testing.T, req jiratest.Request) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(req.Body), &out); err != nil {
		t.Fatalf("decode %s %s body %q: %v", req.Method, req.Path, req.Body, err)
	}
	return out
}

// jsonOf re-encodes v compactly with sorted keys, for comparisons.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func noWrites(t *testing.T, fake *jiratest.Fake) {
	t.Helper()
	for _, r := range fake.Requests() {
		if r.Method != "GET" {
			t.Errorf("unexpected write %s %s", r.Method, r.Path)
		}
	}
}

func jsonDecode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
