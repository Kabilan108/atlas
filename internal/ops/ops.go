// Package ops defines operations once so the CLI and the MCP server expose
// the same behaviour. Ops never touch the terminal, git, or local config: each
// front end supplies those through Env and the op's input.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// Surface says which front ends expose an op.
type Surface uint8

const (
	CLI Surface = 1 << iota
	MCP
	Both = CLI | MCP
)

// Has reports whether s includes every surface in other.
func (s Surface) Has(other Surface) bool {
	return s&other == other
}

// Meta describes an op to both front ends. Name is the MCP tool name.
type Meta struct {
	Name        string
	Description string
	Surfaces    Surface
	ReadOnly    bool
	Destructive bool
}

// Env carries per-call credentials and defaults. A CLI run or an MCP call
// builds a fresh Env, and nothing in it outlives the call.
type Env interface {
	Bitbucket(ctx context.Context) (*bitbucket.Client, error)
	DefaultWorkspace() string
}

// Result is an op's output in both forms: the typed value and the text an
// agent reads.
type Result struct {
	Value any
	Text  string
}

// Def is an op with its types erased, so front ends can register a list of
// them in one loop.
type Def interface {
	Info() Meta
	InputType() reflect.Type
	Invoke(ctx context.Context, env Env, args json.RawMessage) (Result, error)
}

// Op is one operation. Markdown renders Out for agents; when it is nil the
// text form is compact JSON.
type Op[In, Out any] struct {
	Meta
	Run      func(ctx context.Context, env Env, in In) (Out, error)
	Markdown func(Out) (string, error)
}

func (o *Op[In, Out]) Info() Meta {
	return o.Meta
}

func (o *Op[In, Out]) InputType() reflect.Type {
	return reflect.TypeFor[In]()
}

// Call runs the op with typed input. CLI bindings use it.
func (o *Op[In, Out]) Call(ctx context.Context, env Env, in In) (Out, error) {
	return o.Run(ctx, env, in)
}

// Text renders out the way MCP returns it.
func (o *Op[In, Out]) Text(out Out) (string, error) {
	if o.Markdown != nil {
		return o.Markdown(out)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode %s result: %w", o.Name, err)
	}
	return string(data), nil
}

// Invoke decodes JSON arguments, runs the op, and renders the result. A
// Partial error comes back with the result rendered so far.
func (o *Op[In, Out]) Invoke(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	in, err := decodeArgs[In](args)
	if err != nil {
		return Result{}, err
	}
	out, runErr := o.Run(ctx, env, in)
	if runErr != nil && KindOf(runErr) != Partial {
		return Result{}, runErr
	}
	text, err := o.Text(out)
	if err != nil {
		return Result{}, errors.Join(runErr, err)
	}
	return Result{Value: out, Text: text}, runErr
}

func decodeArgs[In any](args json.RawMessage) (In, error) {
	var in In
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return in, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, Errorf(Invalid, "invalid arguments: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return in, Errorf(Invalid, "invalid arguments: trailing data after JSON object")
	}
	return in, nil
}
