package httpx

import (
	"reflect"
	"strconv"
	"strings"
)

// readRules documents the rules of the validate tag of a request field, and says whether they make
// it required. A pointer accepts null, as the validator treats it as unset; a field the validator
// requires rejects null, so it opts out with `nullable:"false"`. Unknown rules are left out.
func (d *fieldDoc) readRules() bool {
	if d.field.Type.Kind() == reflect.Pointer && d.field.Tag.Get("nullable") != "false" {
		d.nullable = true
	}
	rules := strings.Split(d.field.Tag.Get("validate"), ",")
	reader := newRuleReader(d.schema, d.field.Type, rules)
	for _, rule := range rules {
		reader.read(rule)
	}
	if reader.required {
		d.markRequired()
	}
	return reader.required
}

// markRequired documents what the validator's required rule refuses: a nil list, and an empty
// string. It permits an allocated empty list and a non-nil string pointer.
func (d *fieldDoc) markRequired() {
	switch d.field.Type.Kind() {
	case reflect.Slice:
		d.nullable = false
	case reflect.String:
		if minimum, ok := d.schema["minLength"].(int); !ok || minimum < 1 {
			d.schema["minLength"] = 1
		}
	}
}

// ruleReader reads the rules of a validate tag onto the schema of the field, or of the items of
// the field's list after a dive
type ruleReader struct {
	schema    map[string]any
	valueType reflect.Type
	kind      reflect.Kind
	// optional and required are what the rules before a dive say of the field itself
	optional, required bool
	// diving is whether the rules now read are the items'
	diving bool
}

func newRuleReader(schema map[string]any, fieldType reflect.Type, rules []string) *ruleReader {
	reader := &ruleReader{schema: schema, valueType: derefType(fieldType)}
	reader.kind = reader.valueType.Kind()
	for _, rule := range rules {
		if rule == "dive" {
			break
		}
		reader.optional = reader.optional || rule == "omitempty"
		reader.required = reader.required || rule == "required"
	}
	return reader
}

// read documents one rule of the tag
func (r *ruleReader) read(rule string) {
	key, value, _ := strings.Cut(rule, "=")
	switch key {
	case "dive":
		r.dive()
	case "oneof":
		r.oneOf(value)
	case "email":
		r.schema["format"] = "email"
	case "hexcolor":
		r.schema["pattern"] = "^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"
	case "min", "max":
		r.limit(key, value)
	}
}

// dive moves to the items of a list, for the rules after it
func (r *ruleReader) dive() {
	items, ok := r.schema["items"].(map[string]any)
	if ok && (r.kind == reflect.Slice || r.kind == reflect.Array) {
		r.schema = items
		r.valueType = derefType(r.valueType.Elem())
		r.kind, r.diving = r.valueType.Kind(), true
	}
}

// oneOf documents the choices of a oneof rule as an enum. The field's null is added when the schema
// is finished; items that may be null get it here.
func (r *ruleReader) oneOf(choices string) {
	var enum []any
	for _, choice := range strings.Fields(choices) {
		enum = append(enum, choice)
	}
	if types, ok := r.schema["type"].([]string); ok && r.diving && len(types) > 1 {
		enum = append(enum, nil)
	}
	r.schema["enum"] = enum
}

// limit documents a min or max rule as the length of a string, the size of a list or the value of
// a number. A limit that isn't a number is left out.
func (r *ruleReader) limit(key, value string) {
	limit, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return
	}
	isMin := key == "min"
	if isMin && limit > 0 && !r.optional && !r.diving {
		r.required = true
	}
	names, counted := countKeywords[r.kind]
	if !counted {
		r.schema[pick(isMin, "minimum", "maximum")] = limit
		return
	}
	r.schema[pick(isMin, names[0], names[1])] = int(limit)
}

// countKeywords are the keywords that limit the length of a string and the size of a list
var countKeywords = map[reflect.Kind][2]string{reflect.String: {"minLength", "maxLength"}, reflect.Slice: {"minItems", "maxItems"}}

// pick is lower for a min rule and upper for a max rule
func pick(isMin bool, lower, upper string) string {
	if isMin {
		return lower
	}
	return upper
}
