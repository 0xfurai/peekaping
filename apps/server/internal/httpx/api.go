package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"peekaping/internal/version"
	"reflect"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// NoBody marks operations that do not consume a request body.
type NoBody struct{}

// RetryAfterHeader documents the Retry-After header of a 429 (see TooManyRequests)
var RetryAfterHeader = &huma.Param{Description: "Seconds to wait before sending the request again.", Schema: &huma.Schema{Type: "integer"}}

var JWTOnly = []map[string][]string{{"JwtAuth": {}}}
var SharedAuth = []map[string][]string{{"JwtAuth": {}}, {"ApiKeyAuth": {}}}

// WorkspaceIDHeader is the header that names the workspace a request runs in. A route group whose
// records belong to a workspace documents it with Router.DocumentWorkspace, next to the middleware
// that reads it.
var WorkspaceIDHeader = &huma.Param{
	Name:        "X-Workspace-ID",
	In:          "header",
	Description: "The workspace the request runs in. Without it, the user's default workspace, the one they joined first. An API key always runs in its own workspace.",
	Schema:      &huma.Schema{Type: "string", Format: "uuid"},
}

// ProbeAuth is the probe token, which opens the probe protocol's routes and nothing else
var ProbeAuth = []map[string][]string{{"ProbeAuth": {}}}

// Input keeps the typed body and parameter contract together. Decoding stays
// explicit in the handler: existing endpoints check authentication and sometimes
// the resource before decoding, and accept one JSON value with unknown fields.
// Huma's automatic binding would change those observable behaviors.
type Input[B, P any] struct {
	Request *http.Request `json:"-"`
}

func (i *Input[B, P]) Resolve(ctx huma.Context) []error {
	i.Request, _ = humago.Unwrap(ctx)
	return nil
}

func (i *Input[B, P]) Decode() (B, error) {
	var body B
	err := DecodeJSON(i.Request, &body)
	return body, err
}

func (*Input[B, P]) bodyType() reflect.Type   { return reflect.TypeFor[B]() }
func (*Input[B, P]) paramsType() reflect.Type { return reflect.TypeFor[P]() }

type inputContract interface {
	bodyType() reflect.Type
	paramsType() reflect.Type
}

// Output is serialized by Huma, with the existing API's status and media type.
type Output[T any] struct {
	Status       int
	ContentType  *string `header:"Content-Type"`
	CacheControl *string `header:"Cache-Control"`
	Body         T
}

func Reply[T any](status int, body T) (*Output[T], error) {
	output := &Output[T]{Status: status, Body: body}
	if status != http.StatusNoContent && status != http.StatusNotModified {
		contentType := "application/json; charset=utf-8"
		output.ContentType = &contentType
	}
	return output, nil
}

// EmptyBody is the body of a response that has none: the status alone is the answer. Huma writes a
// []byte body as it is, so an empty one sends nothing, where NoBody would send "{}". The
// specification documents such a response without content.
type EmptyBody = []byte

// Empty is a response with a status and no body, not even a Content-Type.
func Empty(status int) (*Output[EmptyBody], error) {
	return &Output[EmptyBody]{Status: status}, nil
}

// SVGBody is raw SVG, with a string schema and its own response media type.
type SVGBody []byte

func (SVGBody) Schema(huma.Registry) *huma.Schema { return &huma.Schema{Type: "string"} }
func (SVGBody) ContentType(string) string         { return "image/svg+xml" }

func SVG(svg string) (*Output[SVGBody], error) {
	cache := "no-cache, no-store, must-revalidate"
	contentType := "image/svg+xml"
	return &Output[SVGBody]{Status: 200, ContentType: &contentType, CacheControl: &cache, Body: SVGBody(svg)}, nil
}

// ResponseError preserves existing error bodies, including the endpoint-specific
// domain and 2FA responses. Error-model unification belongs to PP-87.
type ResponseError struct {
	Status int
	Body   any
}

func (e *ResponseError) Error() string                { return http.StatusText(e.Status) }
func (e *ResponseError) GetStatus() int               { return e.Status }
func (e *ResponseError) ContentType(string) string    { return "application/json; charset=utf-8" }
func (e *ResponseError) MarshalJSON() ([]byte, error) { return json.Marshal(e.Body) }
func Fail(status int, body any) error                 { return &ResponseError{Status: status, Body: body} }

type ErrorBody struct {
	Message string `json:"message"`
	Data    Null   `json:"data"`
}

// Null documents the data field emitted by existing error responses.
type Null struct{}

func (Null) Schema(huma.Registry) *huma.Schema { return &huma.Schema{Type: "null"} }
func (Null) MarshalJSON() ([]byte, error)      { return []byte("null"), nil }

// muxBridge routes Huma registrations through the same compatibility and
// middleware layer as other standard HTTP handlers.
type muxBridge struct{ router *Router }

func (m muxBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.router.ServeHTTP(w, r) }
func (m muxBridge) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		panic("Huma registered a route without a method: " + pattern)
	}
	router := *m.router
	router.prefix = ""
	router.Handle(method, path, handler)
}

// operationAPI customizes registration, not a second specification pipeline.
// Huma still derives output schemas and serves/exports this same OpenAPI object.
type operationAPI struct {
	huma.API
	router *Router
	input  inputContract
	// parameters are the ones the router documents on every operation (Router.Document)
	parameters []*huma.Param
	// empty is set when the handler answers with an EmptyBody
	empty bool
}

func (a operationAPI) Adapter() huma.Adapter { return humago.NewAdapter(muxBridge{a.router}, "") }

// Existing endpoints always produce JSON regardless of Accept.
func (a operationAPI) Negotiate(string) (string, error) { return "application/json", nil }
func (a operationAPI) Marshal(w io.Writer, _ string, value any) error {
	if svg, ok := value.(SVGBody); ok {
		_, err := w.Write(svg)
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
func (a operationAPI) DocumentOperation(op *huma.Operation) {
	registry := a.OpenAPI().Components.Schemas
	if body := a.input.bodyType(); body != reflect.TypeFor[NoBody]() {
		schema := registry.Schema(body, true, op.OperationID+"Request")
		normalizeRequest(registry, body)
		op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			"application/json": {Schema: schema},
		}}
	}
	a.documentParameters(op, registry)
	documentResponses(op, a.empty)
	for _, schema := range registry.Map() {
		documentNull(schema)
	}
	a.OpenAPI().AddOperation(op)
}

// documentParameters adds the parameters of the handler's params struct, then the ones the router
// documents on every operation
func (a operationAPI) documentParameters(op *huma.Operation, registry huma.Registry) {
	params := a.input.paramsType()
	for n := 0; n < params.NumField(); n++ {
		if parameter, ok := parameterOf(params.Field(n), op, registry); ok {
			op.Parameters = append(op.Parameters, parameter)
		}
	}
	for _, parameter := range a.parameters {
		documented := *parameter
		op.Parameters = append(op.Parameters, &documented)
	}
}

// parameterOf documents a field of the params struct: a path parameter by its path tag, any other
// by its query tag. A field with neither is not a parameter.
func parameterOf(f reflect.StructField, op *huma.Operation, registry huma.Registry) (*huma.Param, bool) {
	location, name := "query", f.Tag.Get("query")
	if path := f.Tag.Get("path"); path != "" {
		location, name = "path", path
	}
	// A handler may also serve an alias without its optional path segment.
	if name == "" || (location == "path" && !strings.Contains(op.Path, "{"+name+"}")) {
		return nil, false
	}
	return &huma.Param{Name: name, In: location,
		Required: location == "path" || f.Tag.Get("required") == "true", Description: f.Tag.Get("doc"),
		Schema: huma.SchemaFromField(registry, f, op.OperationID+f.Name)}, true
}

// documentResponses drops the default response, the bodies of the responses that have none, and
// the headers Huma adds to a success response. An empty handler answers every success without one.
func documentResponses(op *huma.Operation, empty bool) {
	delete(op.Responses, "default")
	for code, response := range op.Responses {
		if code == "204" || code == "304" || (empty && strings.HasPrefix(code, "2")) {
			response.Content = nil
			response.Headers = nil
			continue
		}
		if strings.HasPrefix(code, "2") {
			delete(response.Headers, "Content-Type")
			if response.Content["image/svg+xml"] == nil {
				delete(response.Headers, "Cache-Control")
			}
		}
	}
}

// documentNull finishes schema and every schema in it, properties and list items alike, when they
// are nullable. Huma marks a `nullable:"true"` field nullable but leaves null out of its enum, and
// it marks the items of a list of pointers nullable the same way.
func documentNull(schema *huma.Schema) {
	if schema == nil {
		return
	}
	if schema.Nullable {
		allowNull(schema)
	}
	for _, property := range schema.Properties {
		documentNull(property)
	}
	documentNull(schema.Items)
}

// allowNull documents null as a value of property. An enum lists null beside its values. A $ref has
// no type to add null to, so it becomes anyOf the $ref and null. A property without a type, such as
// one allowNull already turned into anyOf, is left as it is.
func allowNull(property *huma.Schema) {
	if property.Ref != "" {
		property.AnyOf = []*huma.Schema{{Ref: property.Ref}, {Type: "null"}}
		property.Ref, property.Nullable = "", false
		return
	}
	if property.Type == "" {
		property.Nullable = false
		return
	}
	property.Nullable = true
	for _, value := range property.Enum {
		if value == nil {
			return
		}
	}
	if len(property.Enum) > 0 {
		property.Enum = append(property.Enum, nil)
	}
}

func Register[I, T any](router *Router, method, path string, op huma.Operation, handler func(context.Context, *I) (*Output[T], error), middleware ...Middleware) {
	api := router.API()
	op.Method, op.Path = method, join(router.prefix, path)
	if op.OperationID == "" {
		op.OperationID = operationID(method, op.Path)
	}
	if op.DefaultStatus == 0 {
		op.DefaultStatus = 200
	}
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	// Explicit error responses avoid Huma's Problem Details/422 defaults.
	for _, status := range append(op.Errors, router.errors...) {
		if op.Responses[strconv.Itoa(status)] != nil {
			continue
		}
		response := &huma.Response{Description: http.StatusText(status), Content: map[string]*huma.MediaType{
			"application/json": {Schema: router.registry.api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")},
		}}
		if status == http.StatusTooManyRequests {
			response.Headers = map[string]*huma.Param{"Retry-After": RetryAfterHeader}
		}
		op.Responses[strconv.Itoa(status)] = response
	}
	op.Errors = nil
	scoped := *router
	scoped.middleware = append(append([]Middleware(nil), router.middleware...), middleware...)
	input, ok := any(new(I)).(inputContract)
	if !ok {
		panic("REST handlers must use httpx.Input")
	}
	huma.Register(operationAPI{API: api, router: &scoped, input: input, parameters: router.parameters, empty: reflect.TypeFor[T]() == reflect.TypeFor[EmptyBody]()}, op, handler)
}

// ErrorResponse and ErrorVariants describe the few established responses that
// differ from the normal {message,data:null} envelope.
func ErrorResponse[T any](router *Router, status int) *huma.Response {
	return &huma.Response{Description: http.StatusText(status), Content: map[string]*huma.MediaType{
		"application/json": {Schema: router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[T](), true, "Error")},
	}}
}
func ErrorVariants[T any](router *Router, status int) *huma.Response {
	response := ErrorResponse[T](router, status)
	media := response.Content["application/json"]
	media.Schema = &huma.Schema{AnyOf: []*huma.Schema{media.Schema,
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")}}
	return response
}

// ErrorVariants2 is ErrorVariants for an operation that answers with either of two error bodies
// besides the usual one.
func ErrorVariants2[T, U any](router *Router, status int) *huma.Response {
	response := ErrorResponse[T](router, status)
	media := response.Content["application/json"]
	media.Schema = &huma.Schema{AnyOf: []*huma.Schema{media.Schema,
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[U](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")}}
	return response
}

// ErrorVariants3 is ErrorVariants for an operation that answers with any of three error bodies
// besides the usual one.
func ErrorVariants3[T, U, V any](router *Router, status int) *huma.Response {
	response := ErrorResponse[T](router, status)
	media := response.Content["application/json"]
	media.Schema = &huma.Schema{AnyOf: []*huma.Schema{media.Schema,
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[U](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[V](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")}}
	return response
}

// ErrorVariants4 is ErrorVariants for an operation that answers with any of four error bodies
// besides the usual one.
func ErrorVariants4[T, U, V, W any](router *Router, status int) *huma.Response {
	response := ErrorResponse[T](router, status)
	media := response.Content["application/json"]
	media.Schema = &huma.Schema{AnyOf: []*huma.Schema{media.Schema,
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[U](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[V](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[W](), true, "Error"),
		router.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")}}
	return response
}

// normalizeRequest documents the existing validator's rules without enabling
// Huma validation. In particular, untagged fields are optional and unknown JSON
// fields are accepted. Runtime validators and their messages remain unchanged.
func normalizeRequest(registry huma.Registry, t reflect.Type) {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return
	}
	ref := registry.Schema(t, true, t.Name()).Ref
	schema := registry.Map()[strings.TrimPrefix(ref, "#/components/schemas/")]
	if schema == nil {
		return
	}
	schema.Required = nil
	schema.AdditionalProperties = true
	for n := 0; n < t.NumField(); n++ {
		f := t.Field(n)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		property := schema.Properties[name]
		if property == nil {
			continue
		}
		normalizeField(schema, name, f, property)
	}
}

// derefType is t, or what t points to, however many pointers deep
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// normalizeField documents the rules of one field of a request, named name in schema
func normalizeField(schema *huma.Schema, name string, f reflect.StructField, property *huma.Schema) {
	// A pointer field accepts null, as the validator treats it as unset. A field the validator
	// requires rejects null, so it opts out with `nullable:"false"`.
	if f.Type.Kind() == reflect.Pointer && f.Tag.Get("nullable") != "false" {
		allowNull(property)
	}
	rules := strings.Split(f.Tag.Get("validate"), ",")
	reader := newRuleReader(property, f.Type, rules)
	for _, rule := range rules {
		reader.read(rule)
	}
	if reader.required {
		markRequired(schema, name, f)
	}
}

// ruleReader reads the rules of a validate tag onto the schema of the field, or of the items of
// the field's list after a dive
type ruleReader struct {
	property  *huma.Schema
	valueType reflect.Type
	kind      reflect.Kind
	// optional and required are what the rules before a dive say of the field itself
	optional, required bool
	// diving is whether the rules now read are the items'
	diving bool
}

func newRuleReader(property *huma.Schema, fieldType reflect.Type, rules []string) *ruleReader {
	reader := &ruleReader{property: property, valueType: derefType(fieldType)}
	reader.kind = reader.valueType.Kind()
	for _, rule := range rules {
		if rule == "dive" {
			break
		}
		if rule == "omitempty" {
			reader.optional = true
		}
		if rule == "required" {
			reader.required = true
		}
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
		r.property.Format = "email"
	case "hexcolor":
		r.property.Pattern = "^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"
	case "min", "max":
		r.limit(key, value)
	}
}

// dive moves to the items of a list, for the rules after it
func (r *ruleReader) dive() {
	if r.property.Items != nil && (r.kind == reflect.Slice || r.kind == reflect.Array) {
		r.property = r.property.Items
		r.valueType = derefType(r.valueType.Elem())
		r.kind, r.diving = r.valueType.Kind(), true
	}
}

// oneOf documents the choices of a oneof rule as an enum, with null when the value may be null
func (r *ruleReader) oneOf(choices string) {
	r.property.Enum = nil
	for _, choice := range strings.Fields(choices) {
		r.property.Enum = append(r.property.Enum, choice)
	}
	if r.property.Nullable {
		r.property.Enum = append(r.property.Enum, nil)
	}
}

// limit documents a min or max rule as the length of a string, the size of a list or the value of
// a number. A limit that isn't a number is left out.
func (r *ruleReader) limit(key, value string) {
	limit, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return
	}
	isMin := key == "min"
	if isMin && r.minRequires(limit) {
		r.required = true
	}
	switch r.kind {
	case reflect.String:
		setBound(isMin, &r.property.MinLength, &r.property.MaxLength, int(limit))
	case reflect.Slice:
		setBound(isMin, &r.property.MinItems, &r.property.MaxItems, int(limit))
	default:
		setBound(isMin, &r.property.Minimum, &r.property.Maximum, limit)
	}
}

// minRequires is whether a min rule of limit makes the field required: a positive minimum does,
// unless the field is optional or the rule is an item's
func (r *ruleReader) minRequires(limit float64) bool {
	return limit > 0 && !r.optional && !r.diving
}

// setBound points lower at v for a min rule and upper at v for a max rule
func setBound[T any](isMin bool, lower, upper **T, v T) {
	if isMin {
		*lower = &v
	} else {
		*upper = &v
	}
}

// markRequired lists the field as required in schema
func markRequired(schema *huma.Schema, name string, f reflect.StructField) {
	schema.Required = append(schema.Required, name)
	// Validator's required rejects nil slices and empty value strings,
	// but permits allocated empty slices and non-nil string pointers.
	parent := schema.Properties[name]
	if f.Type.Kind() == reflect.Slice {
		parent.Nullable = false
	}
	if f.Type.Kind() == reflect.String && (parent.MinLength == nil || *parent.MinLength < 1) {
		minimum := 1
		parent.MinLength = &minimum
	}
}

func (r *Router) API() huma.API {
	if r.registry.api == nil {
		r.registry.api = newAPI(&Router{registry: r.registry})
	}
	return r.registry.api
}

func operationID(method, path string) string {
	id := strings.ToLower(method)
	if probePath, ok := strings.CutPrefix(path, "/probe/v1"); ok {
		// The probe protocol's ids start with probe, so that they can't collide with the API's
		// (probePostHello, probeGetAssignments)
		path = probePath
		id = "probe" + strings.ToUpper(id[:1]) + id[1:]
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1"), "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "{") {
			id += "By"
			part = strings.Trim(part, "{}")
		}
		for _, word := range strings.Split(part, "-") {
			id += strings.ToUpper(word[:1]) + word[1:]
		}
	}
	if path == "/health" {
		id += "Root"
	}
	return id
}

func newAPI(router *Router) huma.API {
	config := huma.DefaultConfig("Peekaping API", version.Version)
	config.Servers = []*huma.Server{{URL: "/"}}
	config.Security = []map[string][]string{}
	config.SchemasPath = ""
	config.CreateHooks = nil
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"JwtAuth":    {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
		"ProbeAuth":  {Type: "http", Scheme: "bearer", BearerFormat: "probe token"},
	}
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Name() == "" {
			return huma.DefaultSchemaNamer(t, hint)
		}
		name := strings.ReplaceAll(strings.ReplaceAll(t.String(), "peekaping/internal/modules/", ""), " ", "")
		return strings.NewReplacer("shared.Monitor", "monitor.Model", "shared.Proxy", "proxy.Model", "shared.HeartBeatModel", "heartbeat.Model",
			"interface{}", "Any", "[]", "Array_", "[", "_", "]", "", "*", "", "/", "_", ",", "_", " ", "").Replace(name)
	})
	config.Formats = map[string]huma.Format{"application/json": {
		Marshal: func(w io.Writer, v any) error {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		},
		Unmarshal: json.Unmarshal,
	}}
	return humago.New(muxBridge{router}, config)
}
