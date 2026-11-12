package ops_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

func TestKindOf(t *testing.T) {
	tests := []struct {
		err  error
		want ops.Kind
	}{
		{nil, ops.Unknown},
		{errors.New("plain"), ops.Unknown},
		{ops.Errorf(ops.Invalid, "bad"), ops.Invalid},
		{fmt.Errorf("wrapped: %w", ops.Errorf(ops.Partial, "half")), ops.Partial},
		{&bitbucket.APIError{StatusCode: 400}, ops.Invalid},
		{&bitbucket.APIError{StatusCode: 401}, ops.Auth},
		{&bitbucket.APIError{StatusCode: 403}, ops.Auth},
		{fmt.Errorf("fetch: %w", &bitbucket.APIError{StatusCode: 404}), ops.NotFound},
		{&bitbucket.APIError{StatusCode: 409}, ops.Conflict},
		{&bitbucket.APIError{StatusCode: 429}, ops.RateLimited},
		{&bitbucket.APIError{StatusCode: 502}, ops.Upstream},
		{ops.Errorf(ops.Upstream, "context: %w", &bitbucket.APIError{StatusCode: 404}), ops.Upstream},
		{&jira.APIError{StatusCode: 401}, ops.Auth},
		{fmt.Errorf("edit: %w", &jira.APIError{StatusCode: 409}), ops.Conflict},
		{&jira.APIError{StatusCode: 404}, ops.NotFound},
		{&jira.APIError{StatusCode: 400}, ops.Invalid},
		{&jira.APIError{StatusCode: 503}, ops.Upstream},
		{statusError(408), ops.Timeout},
		{statusError(422), ops.Invalid},
		{context.DeadlineExceeded, ops.Timeout},
		{fmt.Errorf("poll: %w", context.DeadlineExceeded), ops.Timeout},
		{&url.Error{Op: "Get", URL: "https://x", Err: os.ErrDeadlineExceeded}, ops.Timeout},
		{context.Canceled, ops.Unknown},
	}
	for _, tc := range tests {
		if got := ops.KindOf(tc.err); got != tc.want {
			t.Errorf("KindOf(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// statusError is an API error from neither client, classified only through
// HTTPStatus.
type statusError int

func (s statusError) Error() string   { return fmt.Sprintf("status %d", int(s)) }
func (s statusError) HTTPStatus() int { return int(s) }

func TestKindStrings(t *testing.T) {
	want := map[ops.Kind]string{
		ops.Unknown: "unknown", ops.Invalid: "invalid", ops.Auth: "auth", ops.NotFound: "not_found",
		ops.Conflict: "conflict", ops.RateLimited: "rate_limited", ops.Timeout: "timeout",
		ops.Upstream: "upstream", ops.Partial: "partial",
	}
	for kind, name := range want {
		if kind.String() != name {
			t.Errorf("%d.String() = %q, want %q", kind, kind.String(), name)
		}
	}
}

type echoInput struct {
	Items []string `json:"items"`
}

type echoOutput struct {
	Done []string `json:"done"`
}

// echo fails after the first item, returning what it finished.
var echo = &ops.Op[echoInput, echoOutput]{
	Meta: ops.Meta{Name: "echo", Surfaces: ops.Both},
	Run: func(_ context.Context, _ ops.Env, in echoInput) (echoOutput, error) {
		if len(in.Items) > 1 {
			return echoOutput{Done: in.Items[:1]}, ops.Errorf(ops.Partial, "stopped after %d of %d", 1, len(in.Items))
		}
		return echoOutput{Done: in.Items}, nil
	},
}

func TestInvokeRendersJSONWithoutMarkdown(t *testing.T) {
	result, err := echo.Invoke(context.Background(), nil, json.RawMessage(`{"items":["a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != `{"done":["a"]}` {
		t.Fatalf("text = %s", result.Text)
	}
}

func TestInvokeKeepsPartialResults(t *testing.T) {
	result, err := echo.Invoke(context.Background(), nil, json.RawMessage(`{"items":["a","b"]}`))
	if ops.KindOf(err) != ops.Partial {
		t.Fatalf("err = %v, want partial", err)
	}
	if result.Text != `{"done":["a"]}` {
		t.Fatalf("partial text = %q", result.Text)
	}
}

func TestSurfaceHas(t *testing.T) {
	if !ops.Both.Has(ops.MCP) || !ops.Both.Has(ops.CLI) || ops.CLI.Has(ops.MCP) || ops.MCP.Has(ops.Both) {
		t.Fatal("Surface.Has is wrong")
	}
}
