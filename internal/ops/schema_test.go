package ops_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
)

type reviewAction string

func (reviewAction) Enum() []string { return []string{"approve", "request-changes", "comment"} }

type side int

func (side) Enum() []string { return []string{"0", "1"} }

type reviewComment struct {
	Path string       `json:"path" jsonschema:"file path"`
	Line int          `json:"line,omitempty"`
	Kind reviewAction `json:"kind,omitempty"`
}

type reviewInput struct {
	Action   reviewAction    `json:"action" jsonschema:"final review action"`
	Also     []reviewAction  `json:"also,omitempty"`
	Maybe    *reviewAction   `json:"maybe,omitempty"`
	Side     side            `json:"side,omitempty"`
	Comments []reviewComment `json:"comments,omitempty"`
	Labels   map[string]int  `json:"labels,omitempty"`
	Draft    *bool           `json:"draft,omitempty"`
	Local    string          `json:"-"`
}

var review = &ops.Op[reviewInput, reviewInput]{
	Meta: ops.Meta{Name: "review", Surfaces: ops.Both},
	Run:  func(_ context.Context, _ ops.Env, in reviewInput) (reviewInput, error) { return in, nil },
}

func TestInputSchemaEnumsAndNesting(t *testing.T) {
	schema, err := ops.InputSchema(review.InputType())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	actions := []any{"approve", "request-changes", "comment"}
	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"action"},
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": actions, "description": "final review action"},
			"also":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": actions}},
			"maybe":  map[string]any{"type": "string", "enum": actions},
			"side":   map[string]any{"type": "integer", "enum": []any{0.0, 1.0}},
			"comments": map[string]any{"type": "array", "items": map[string]any{
				"type":     "object",
				"required": []any{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "file path"},
					"line": map[string]any{"type": "integer"},
					"kind": map[string]any{"type": "string", "enum": actions},
				},
			}},
			"labels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer"}},
			"draft":  map[string]any{"type": "boolean"},
		},
	}
	if !reflect.DeepEqual(got, any(want)) {
		t.Fatalf("schema = %s", data)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("schema still offers null: %s", data)
	}
}

func TestEnumsAreValidatedOnBothPaths(t *testing.T) {
	tests := []struct {
		name string
		in   reviewInput
		want string
	}{
		{"top level", reviewInput{Action: "merge"}, `invalid action "merge": expected one of approve, request-changes, comment`},
		{"slice", reviewInput{Action: "approve", Also: []reviewAction{"comment", "nope"}}, `invalid also[1] "nope"`},
		{"pointer", reviewInput{Action: "approve", Maybe: ptr(reviewAction("x"))}, `invalid maybe "x"`},
		{"nested", reviewInput{Action: "approve", Comments: []reviewComment{{Path: "a", Kind: "x"}}}, `invalid comments[0].kind "x"`},
		{"integer", reviewInput{Action: "approve", Side: 7}, `invalid side "7"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := review.Call(context.Background(), nil, tc.in)
			if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Call error = %v, want invalid %q", err, tc.want)
			}
			args, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := review.Invoke(context.Background(), nil, args); ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Invoke error = %v, want invalid %q", err, tc.want)
			}
		})
	}
	if _, err := review.Call(context.Background(), nil, reviewInput{Action: "comment", Maybe: ptr(reviewAction("approve"))}); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	if _, err := review.Call(context.Background(), nil, reviewInput{}); err != nil {
		t.Fatalf("zero enums are left to the op: %v", err)
	}
}

type labeledAction string

func (labeledAction) Enum() []string   { return []string{"approve", "comment"} }
func (a labeledAction) String() string { return "action:" + string(a) }

type labeledSide int

func (labeledSide) Enum() []string { return []string{"0", "1"} }
func (s labeledSide) String() string {
	if s == 1 {
		return "right"
	}
	return "left"
}

type labeledInput struct {
	Action labeledAction `json:"action"`
	Side   labeledSide   `json:"side,omitempty"`
}

// Enums are checked by their value, never by what String() displays.
func TestEnumsWithStringMethodsValidateByValue(t *testing.T) {
	op := &ops.Op[labeledInput, labeledInput]{
		Meta: ops.Meta{Name: "labeled", Surfaces: ops.Both},
		Run:  func(_ context.Context, _ ops.Env, in labeledInput) (labeledInput, error) { return in, nil },
	}
	valid := labeledInput{Action: "approve", Side: 1}
	if _, err := op.Call(context.Background(), nil, valid); err != nil {
		t.Fatalf("Call rejected a valid input: %v", err)
	}
	if _, err := op.Invoke(context.Background(), nil, json.RawMessage(`{"action":"approve","side":1}`)); err != nil {
		t.Fatalf("Invoke rejected a valid input: %v", err)
	}
	tests := []struct {
		name string
		in   labeledInput
		want string
	}{
		{"string", labeledInput{Action: "merge"}, `invalid action "merge"`},
		{"integer", labeledInput{Action: "approve", Side: 7}, `invalid side "7"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := op.Call(context.Background(), nil, tc.in); ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Call error = %v, want invalid %q", err, tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

type describedRef struct {
	Repo string `json:"repo,omitempty" jsonschema:"plain"`
	Name string `json:"name,omitempty" jsonschema:"name"`
}

func (describedRef) PropertyDescriptions() map[string]string {
	return map[string]string{"repo": "from the embedded type", "name": "embedded name"}
}

type describedInput struct {
	describedRef
	Other string `json:"other,omitempty"`
}

func (describedInput) PropertyDescriptions() map[string]string {
	return map[string]string{"name": "from the outer type", "missing": "ignored"}
}

func TestInputSchemaPropertyDescriptions(t *testing.T) {
	schema, err := ops.InputSchema(reflect.TypeFor[describedInput]())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for name, property := range schema.Properties {
		got[name] = property.Description
	}
	want := map[string]string{"repo": "from the embedded type", "name": "from the outer type", "other": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("descriptions = %v, want %v", got, want)
	}
}
