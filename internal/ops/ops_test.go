package ops_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
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
		{&bitbucket.APIError{StatusCode: 409}, ops.Invalid},
		{&bitbucket.APIError{StatusCode: 429}, ops.RateLimited},
		{&bitbucket.APIError{StatusCode: 502}, ops.Upstream},
		{ops.Errorf(ops.Upstream, "context: %w", &bitbucket.APIError{StatusCode: 404}), ops.Upstream},
	}
	for _, tc := range tests {
		if got := ops.KindOf(tc.err); got != tc.want {
			t.Errorf("KindOf(%v) = %v, want %v", tc.err, got, tc.want)
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
