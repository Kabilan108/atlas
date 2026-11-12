package ops

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Enum is implemented by named string or integer types with a fixed set of
// values. Input schemas list the values, and Call and Invoke reject any other
// non-zero value before the op runs.
type Enum interface {
	Enum() []string
}

var enumType = reflect.TypeFor[Enum]()

// PropertyDescriber is implemented by input types, usually embedded ones,
// that word a promoted property differently from the type that declares
// it: pr.Ref says when the repo it embeds is required. The shallowest type
// that describes a property wins.
type PropertyDescriber interface {
	PropertyDescriptions() map[string]string
}

var describerType = reflect.TypeFor[PropertyDescriber]()

// InputSchema is the JSON schema MCP clients see for an op input type. It is
// jsonschema-go's schema with three changes that keep tools/list lean and
// accurate: Enum types list their values; a nullable ["null", T] becomes T,
// since omitting a field is how callers leave it unset; and only the top
// level forbids additional properties (the decoder rejects unknown fields at
// every level anyway).
func InputSchema(t reflect.Type) (*jsonschema.Schema, error) {
	enums := map[reflect.Type]*jsonschema.Schema{}
	if err := collectEnums(t, enums, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: enums})
	if err != nil {
		return nil, err
	}
	normalize(schema, true)
	describeProperties(t, schema)
	return schema, nil
}

// describeProperties applies the PropertyDescriber overrides of t and the
// structs it embeds to the top level of schema, deepest first.
func describeProperties(t reflect.Type, schema *jsonschema.Schema) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	type override struct {
		depth int
		names map[string]string
	}
	var overrides []override
	if t.Implements(describerType) {
		overrides = append(overrides, override{0, reflect.Zero(t).Interface().(PropertyDescriber).PropertyDescriptions()})
	}
	for _, field := range reflect.VisibleFields(t) {
		if field.Anonymous && field.Type.Kind() == reflect.Struct && field.Type.Implements(describerType) {
			overrides = append(overrides, override{len(field.Index), reflect.Zero(field.Type).Interface().(PropertyDescriber).PropertyDescriptions()})
		}
	}
	slices.SortStableFunc(overrides, func(a, b override) int { return b.depth - a.depth })
	for _, o := range overrides {
		for name, description := range o.names {
			if property := schema.Properties[name]; property != nil {
				property.Description = description
			}
		}
	}
}

func collectEnums(t reflect.Type, enums map[reflect.Type]*jsonschema.Schema, seen map[reflect.Type]bool) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if seen[t] {
		return nil
	}
	seen[t] = true
	if t.Implements(enumType) {
		schema, err := enumSchema(t)
		if err != nil {
			return err
		}
		enums[t] = schema
		return nil
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return collectEnums(t.Elem(), enums, seen)
	case reflect.Struct:
		for _, field := range reflect.VisibleFields(t) {
			if err := collectEnums(field.Type, enums, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func enumSchema(t reflect.Type) (*jsonschema.Schema, error) {
	values := reflect.Zero(t).Interface().(Enum).Enum()
	schema := &jsonschema.Schema{}
	switch t.Kind() {
	case reflect.String:
		schema.Type = "string"
		for _, value := range values {
			schema.Enum = append(schema.Enum, value)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
		for _, value := range values {
			var n int64
			if _, err := fmt.Sscan(value, &n); err != nil {
				return nil, fmt.Errorf("enum %s: value %q is not an integer", t, value)
			}
			schema.Enum = append(schema.Enum, n)
		}
	default:
		return nil, fmt.Errorf("enum %s: kind %s is not a string or integer", t, t.Kind())
	}
	if len(schema.Enum) == 0 {
		return nil, fmt.Errorf("enum %s has no values", t)
	}
	return schema, nil
}

func normalize(s *jsonschema.Schema, top bool) {
	if s == nil {
		return
	}
	if len(s.Types) == 2 && s.Types[0] == "null" {
		s.Type, s.Types = s.Types[1], nil
	}
	if !top && s.Type == "object" && s.Properties != nil {
		s.AdditionalProperties = nil
	}
	normalize(s.Items, false)
	if s.AdditionalProperties != nil && s.AdditionalProperties.Not == nil {
		normalize(s.AdditionalProperties, false)
	}
	for _, property := range s.Properties {
		normalize(property, false)
	}
}

// validateEnums rejects any non-zero Enum value outside its set, naming the
// field by its JSON path.
func validateEnums(v reflect.Value, path string) error {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Type().Implements(enumType) {
		if v.IsZero() {
			return nil
		}
		values := v.Interface().(Enum).Enum()
		value := enumValue(v)
		if !slices.Contains(values, value) {
			return Errorf(Invalid, "invalid %s %q: expected one of %s", path, value, strings.Join(values, ", "))
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if err := validateEnums(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if err := validateEnums(iter.Value(), fmt.Sprintf("%s[%v]", path, iter.Key())); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for _, field := range reflect.VisibleFields(v.Type()) {
			if !field.IsExported() || field.Anonymous {
				continue
			}
			name := jsonName(field)
			if name == "" {
				continue
			}
			if path != "" {
				name = path + "." + name
			}
			value, err := v.FieldByIndexErr(field.Index)
			if err != nil {
				continue
			}
			if err := validateEnums(value, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// enumValue is the underlying value of a string or integer enum as Enum()
// spells it, ignoring any String method.
func enumValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	default:
		return fmt.Sprint(v.Interface())
	}
}

// jsonName is the field's JSON name, or "" for fields JSON skips. Fields
// the CLI sets (json:"-") are not validated: they never come from callers.
func jsonName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}
	return field.Name
}
