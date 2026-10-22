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

var JWTOnly = []map[string][]string{{"JwtAuth": {}}}
var SharedAuth = []map[string][]string{{"JwtAuth": {}}, {"ApiKeyAuth": {}}}

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
	params := a.input.paramsType()
	for n := 0; n < params.NumField(); n++ {
		f := params.Field(n)
		location, name := "query", f.Tag.Get("query")
		if path := f.Tag.Get("path"); path != "" {
			location, name = "path", path
		}
		// A handler may also serve an alias without its optional path segment.
		if name == "" || (location == "path" && !strings.Contains(op.Path, "{"+name+"}")) {
			continue
		}
		op.Parameters = append(op.Parameters, &huma.Param{Name: name, In: location,
			Required: location == "path" || f.Tag.Get("required") == "true", Description: f.Tag.Get("doc"),
			Schema: huma.SchemaFromField(registry, f, op.OperationID+f.Name)})
	}
	delete(op.Responses, "default")
	for code, response := range op.Responses {
		if code == "204" || code == "304" {
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
	a.OpenAPI().AddOperation(op)
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
	for _, status := range op.Errors {
		if op.Responses[strconv.Itoa(status)] != nil {
			continue
		}
		op.Responses[strconv.Itoa(status)] = &huma.Response{Description: http.StatusText(status), Content: map[string]*huma.MediaType{
			"application/json": {Schema: router.registry.api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[ErrorBody](), true, "Error")},
		}}
	}
	op.Errors = nil
	scoped := *router
	scoped.middleware = append(append([]Middleware(nil), router.middleware...), middleware...)
	input, ok := any(new(I)).(inputContract)
	if !ok {
		panic("REST handlers must use httpx.Input")
	}
	huma.Register(operationAPI{API: api, router: &scoped, input: input}, op, handler)
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

// normalizeRequest documents the existing validator's rules without enabling
// Huma validation. In particular, untagged fields are optional and unknown JSON
// fields are accepted. Runtime validators and their messages remain unchanged.
func normalizeRequest(registry huma.Registry, t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
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
		// A pointer field accepts null, as the validator treats it as unset. A field the validator
		// requires rejects null, so it opts out with `nullable:"false"`.
		if f.Type.Kind() == reflect.Pointer && f.Tag.Get("nullable") != "false" {
			property.Nullable = true
		}
		rules := strings.Split(f.Tag.Get("validate"), ",")
		optional, required := false, false
		for _, rule := range rules {
			if rule == "dive" {
				break
			}
			if rule == "omitempty" {
				optional = true
			}
			if rule == "required" {
				required = true
			}
		}
		valueType := f.Type
		for valueType.Kind() == reflect.Pointer {
			valueType = valueType.Elem()
		}
		kind := valueType.Kind()
		diving := false
		for _, rule := range rules {
			key, value, _ := strings.Cut(rule, "=")
			switch key {
			case "dive":
				if property.Items != nil && (kind == reflect.Slice || kind == reflect.Array) {
					property = property.Items
					valueType = valueType.Elem()
					for valueType.Kind() == reflect.Pointer {
						valueType = valueType.Elem()
					}
					kind, diving = valueType.Kind(), true
				}
			case "oneof":
				property.Enum = nil
				for _, choice := range strings.Fields(value) {
					property.Enum = append(property.Enum, choice)
				}
				if property.Nullable {
					property.Enum = append(property.Enum, nil)
				}
			case "email":
				property.Format = "email"
			case "hexcolor":
				property.Pattern = "^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"
			case "min", "max":
				limit, err := strconv.ParseFloat(value, 64)
				if err != nil {
					continue
				}
				if key == "min" && limit > 0 && !optional && !diving {
					required = true
				}
				if kind == reflect.String {
					v := int(limit)
					if key == "min" {
						property.MinLength = &v
					} else {
						property.MaxLength = &v
					}
				} else if kind == reflect.Slice {
					v := int(limit)
					if key == "min" {
						property.MinItems = &v
					} else {
						property.MaxItems = &v
					}
				} else {
					v := limit
					if key == "min" {
						property.Minimum = &v
					} else {
						property.Maximum = &v
					}
				}
			}
		}
		if required {
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
