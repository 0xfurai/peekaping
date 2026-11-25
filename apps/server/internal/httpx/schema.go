package httpx

import (
	"encoding"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/0xfurai/oasome"
)

// fieldSchema documents a struct field of a body or of the parameters. It reads the schema tags
// (format, default, enum, minimum and the like), gives examples, and says when the field may be
// null. A field of a request body is documented by the rules of its validate tag instead: only
// they make it required.
func (reg *registry) fieldSchema(f *oasome.SchemaField) {
	field := newFieldDoc(f.Field, f.Schema)
	field.readTags()
	switch {
	case f.In != "body":
		field.describe(f.In)
		f.Required = f.Required || f.Field.Tag.Get("required") == "true"
	case reg.requestTypes[f.Struct]:
		f.Required = field.readRules()
	default:
		f.Required = bodyRequired(f.Field, f.Required)
	}
	f.Schema = field.finish()
}

// bodyRequired is whether a field of a body is required: one that is not omitted when empty is,
// unless its required tag says otherwise
func bodyRequired(field reflect.StructField, notOmitted bool) bool {
	if value, ok := field.Tag.Lookup("required"); ok {
		return value == "true"
	}
	return notOmitted
}

// fieldDoc is the schema of a field while it is documented. Whether it may be null is kept apart
// and written last, as it changes the form of the schema.
type fieldDoc struct {
	field    reflect.StructField
	schema   map[string]any
	nullable bool
}

// newFieldDoc starts from the reflected schema. A pointer is not nullable as such: a pointer to a
// scalar is, unless it is omitted when empty, and a list always is.
func newFieldDoc(field reflect.StructField, schema map[string]any) *fieldDoc {
	d := &fieldDoc{field: field, schema: schema, nullable: nullable(field)}
	if field.Type.Kind() == reflect.Pointer {
		d.schema = withoutNull(schema)
	}
	if items, ok := d.schema["items"].(map[string]any); ok && listOfObjectPointers(field.Type) {
		d.schema["items"] = withoutNull(items)
	}
	return d
}

// nullable is whether a field's value may be null: its nullable tag says, or else its type does.
// A pointer that is omitted when empty is never sent as null, unless the tag says it may be.
func nullable(field reflect.StructField) bool {
	tag, tagged := field.Tag.Lookup("nullable")
	if field.Type.Kind() == reflect.Pointer && omitsEmpty(field) && tag != "true" {
		return false
	}
	if tagged {
		return tag == "true"
	}
	return defaultNullable(field)
}

var (
	timeType          = reflect.TypeFor[time.Time]()
	textUnmarshalType = reflect.TypeFor[encoding.TextUnmarshaler]()
	schemaOwnerType   = reflect.TypeFor[interface{ OASSchema() map[string]any }]()
)

// defaultNullable is whether a field's value may be null when no tag says: a list may, and so may
// a pointer to a scalar
func defaultNullable(field reflect.StructField) bool {
	t := derefType(field.Type)
	switch {
	case t.Implements(schemaOwnerType) || reflect.PointerTo(t).Implements(schemaOwnerType):
		return false
	case (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && t.Elem().Kind() != reflect.Uint8:
		return true
	}
	return field.Type.Kind() == reflect.Pointer && isScalar(t)
}

// isScalar is whether values of t are a boolean, a number or a string
func isScalar(t reflect.Type) bool {
	if t == timeType || reflect.PointerTo(t).Implements(textUnmarshalType) {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64:
		return true
	}
	return reflect.Int <= t.Kind() && t.Kind() <= reflect.Uint64
}

// listOfObjectPointers is whether t is a list of pointers to values that are not scalars, whose
// items are not null
func listOfObjectPointers(t reflect.Type) bool {
	t = derefType(t)
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return false
	}
	return t.Elem().Kind() == reflect.Pointer && !isScalar(derefType(t.Elem()))
}

func omitsEmpty(field reflect.StructField) bool {
	return strings.Contains(field.Tag.Get("json"), "omitempty")
}

// withoutNull is schema without the null that the reflection gives a pointer: the schema of the
// value, with the description and example of the field
func withoutNull(schema map[string]any) map[string]any {
	if options, ok := schema["anyOf"].([]any); ok && len(options) == 2 {
		inner, _ := options[0].(map[string]any)
		for key, value := range schema {
			if key != "anyOf" {
				inner[key] = value
			}
		}
		return inner
	}
	if types, ok := schema["type"].([]string); ok {
		schema["type"] = slices.DeleteFunc(slices.Clone(types), func(t string) bool { return t == "null" })[0]
	}
	return schema
}

// keywordTags are the tags that set the schema keyword of the same name, with the parser of their
// values
var keywordTags = []struct {
	names []string
	parse func(string) (any, bool)
}{
	{[]string{"format", "pattern"}, func(v string) (any, bool) { return v, v != "" }},
	{[]string{"minimum", "exclusiveMinimum", "maximum", "exclusiveMaximum", "multipleOf"}, func(v string) (any, bool) {
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	}},
	{[]string{"minLength", "maxLength", "minItems", "maxItems"}, func(v string) (any, bool) {
		count, err := strconv.Atoi(v)
		return count, err == nil
	}},
	{[]string{"uniqueItems", "readOnly", "writeOnly", "deprecated"}, func(v string) (any, bool) { return true, v == "true" }},
}

// readTags documents the schema tags of the field
func (d *fieldDoc) readTags() {
	for _, tags := range keywordTags {
		for _, name := range tags.names {
			if value, ok := tags.parse(d.field.Tag.Get(name)); ok {
				d.schema[name] = value
			}
		}
	}
	if value := d.field.Tag.Get("default"); value != "" {
		d.schema["default"] = tagValue(d.schema, value)
	}
	d.readExample()
	d.readEnum()
}

// readExample gives the example tag as the schema's examples
func (d *fieldDoc) readExample() {
	delete(d.schema, "example")
	if value, ok := d.field.Tag.Lookup("example"); ok {
		d.schema["examples"] = []any{tagValue(d.schema, value)}
	}
}

// readEnum gives the values of the enum tag, separated by commas, to the field or to the items of
// its list
func (d *fieldDoc) readEnum() {
	enum := d.field.Tag.Get("enum")
	if enum == "" {
		return
	}
	target := d.schema
	if items, ok := d.schema["items"].(map[string]any); ok && schemaType(d.schema) == "array" {
		target = items
	}
	var values []any
	for _, value := range strings.Split(enum, ",") {
		values = append(values, tagValue(target, value))
	}
	// The items of a list of pointers may be null
	if types, ok := target["type"].([]string); ok && slices.Contains(types, "null") {
		values = append(values, nil)
	}
	target["enum"] = values
}

// describe repeats the description of a path or query parameter in its schema. A header is
// documented once, on the parameter.
func (d *fieldDoc) describe(in string) {
	if doc := d.field.Tag.Get("doc"); doc != "" && in != "header" {
		d.schema["description"] = doc
	}
}

// tagValue is the value of a tag in the type of schema: a string as it is, a list of strings
// separated by commas, and anything else as JSON
func tagValue(schema map[string]any, value string) any {
	switch schemaType(schema) {
	case "string":
		return value
	case "array":
		items, _ := schema["items"].(map[string]any)
		if schemaType(items) == "string" && !strings.HasPrefix(value, "[") {
			var values []any
			for _, each := range strings.Split(value, ",") {
				values = append(values, strings.TrimSpace(each))
			}
			return values
		}
	}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return value
	}
	return parsed
}

// schemaType is the type of schema besides null, "" when it has none
func schemaType(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []string:
		for _, each := range t {
			if each != "null" {
				return each
			}
		}
	}
	return ""
}

// finish is the schema with null when the field may be null: a $ref becomes anyOf the $ref and
// null, and an enum lists null beside its values
func (d *fieldDoc) finish() map[string]any {
	if !d.nullable {
		return d.schema
	}
	if ref, ok := d.schema["$ref"]; ok {
		delete(d.schema, "$ref")
		d.schema["anyOf"] = []any{map[string]any{"$ref": ref}, map[string]any{"type": "null"}}
		return d.schema
	}
	t, ok := d.schema["type"].(string)
	if !ok {
		return d.schema
	}
	d.schema["type"] = []string{t, "null"}
	if enum, ok := d.schema["enum"].([]any); ok && !slices.Contains(enum, nil) {
		d.schema["enum"] = append(enum, nil)
	}
	return d.schema
}

// derefType is t, or what t points to, however many pointers deep
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
