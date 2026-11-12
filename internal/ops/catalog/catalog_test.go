package catalog_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/catalog"
)

// cliOnlyFields are the input fields hidden from MCP (json:"-"), which only
// the CLI can set, keyed by the type that declares them ("pr.Ref.CurrentBranch"),
// so a field embedded in many ops is listed once. A new one needs a reason
// here; the convention is that only values the CLI reads from the local git
// checkout qualify.
var cliOnlyFields = map[string]string{
	"pr.Ref.CurrentBranch":         "git: the current branch when no PR is given",
	"pr.ChecksInput.CurrentBranch": "git: the current branch when no PR or commit is given",
	// Predates the convention: --json fetches only the PR and its comments,
	// which is all it prints. Revisit when pr_view's JSON form is reworked.
	"pr.ViewInput.JSON": "presentation: --json fetches only what it prints",
}

func TestCLIOnlyInputFieldsAreAllowlisted(t *testing.T) {
	found := map[string]bool{}
	for _, def := range catalog.All() {
		for _, field := range hiddenFields(def.InputType()) {
			key := field
			found[key] = true
			if _, ok := cliOnlyFields[key]; !ok {
				t.Errorf("%s (in %s) is json:\"-\" (CLI-only) but not in cliOnlyFields; MCP callers cannot set it", key, def.Info().Name)
			}
		}
	}
	for key := range cliOnlyFields {
		if !found[key] {
			t.Errorf("cliOnlyFields lists %s, which no op input has; remove it", key)
		}
	}
}

func TestOpNamesAreUniqueAndDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range catalog.All() {
		meta := def.Info()
		if seen[meta.Name] {
			t.Errorf("duplicate op name %s", meta.Name)
		}
		seen[meta.Name] = true
		if meta.Name == "" || strings.ToLower(meta.Name) != meta.Name || strings.ContainsAny(meta.Name, " -") {
			t.Errorf("op name %q must be lower_snake_case", meta.Name)
		}
		if meta.Description == "" || meta.Surfaces == 0 {
			t.Errorf("%s needs a description and at least one surface", meta.Name)
		}
		if meta.ReadOnly && meta.Destructive {
			t.Errorf("%s cannot be both read-only and destructive", meta.Name)
		}
	}
}

// hiddenFields returns the json:"-" fields of t and of the structs it embeds
// or contains.
func hiddenFields(t reflect.Type) []string {
	var fields []string
	var walk func(reflect.Type, map[reflect.Type]bool)
	walk = func(t reflect.Type, seen map[reflect.Type]bool) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for _, field := range reflect.VisibleFields(t) {
			if field.Tag.Get("json") == "-" {
				fields = append(fields, declaringType(t, field.Index).String()+"."+field.Name)
				continue
			}
			if !field.Anonymous {
				walk(field.Type, seen)
			}
		}
	}
	walk(t, map[reflect.Type]bool{})
	return fields
}

// declaringType follows a VisibleFields index path to the struct that declares
// the field, so promoted fields are named by their own type.
func declaringType(t reflect.Type, index []int) reflect.Type {
	for _, i := range index[:len(index)-1] {
		t = t.Field(i).Type
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	return t
}
