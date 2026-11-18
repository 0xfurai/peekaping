package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/0xfurai/oasome"
)

// NoBody marks operations that do not consume a request body.
type NoBody struct{}

// JWTOnly, SharedAuth and ProbeAuth are the security of an operation: each map is one way to sign
// in, and any one of them is enough
var JWTOnly = []map[string][]string{{"JwtAuth": {}}}
var SharedAuth = []map[string][]string{{"JwtAuth": {}}, {"ApiKeyAuth": {}}}

// ProbeAuth is the probe token, which opens the probe protocol's routes and nothing else
var ProbeAuth = []map[string][]string{{"ProbeAuth": {}}}

// retryAfterHeader documents the Retry-After header of a 429 (see TooManyRequests)
var retryAfterHeader = IntHeader("Retry-After", "Seconds to wait before sending the request again.")

// workspaceParams is the header that names the workspace a request runs in. A route group whose
// records belong to a workspace documents it with Router.DocumentWorkspace, next to the middleware
// that reads it.
type workspaceParams struct {
	WorkspaceID string `header:"X-Workspace-ID" format:"uuid" doc:"The workspace the request runs in. Without it, the user's default workspace, the one they joined first. An API key always runs in its own workspace."`
}

// Input keeps the typed body and parameter contract together. Decoding stays explicit in the
// handler: existing endpoints check authentication and sometimes the resource before decoding, and
// accept one JSON value with unknown fields. B documents the request body and P the parameters.
type Input[B, P any] struct {
	Request *http.Request `json:"-"`
}

func (i *Input[B, P]) Decode() (B, error) {
	var body B
	err := DecodeJSON(i.Request, &body)
	return body, err
}

func (i *Input[B, P]) setRequest(r *http.Request) { i.Request = r }
func (*Input[B, P]) bodyType() reflect.Type       { return reflect.TypeFor[B]() }

// document is the request side of the operation: the body, unless it is NoBody, and the parameters
func (*Input[B, P]) document() []oasome.RouteOption {
	options := []oasome.RouteOption{oasome.Params[P]()}
	if reflect.TypeFor[B]() != reflect.TypeFor[NoBody]() {
		options = append(options, oasome.Body[B]())
	}
	return options
}

type inputContract interface {
	setRequest(*http.Request)
	bodyType() reflect.Type
	document() []oasome.RouteOption
}

// Output is a handler's answer: the status, the headers that are set and the body.
type Output[T any] struct {
	Status       int
	ContentType  *string
	CacheControl *string
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

// EmptyBody is the body of a response that has none: the status alone is the answer. A []byte body
// is written as it is, so an empty one sends nothing, where NoBody would send "{}". The
// specification documents such a response without content.
type EmptyBody = []byte

// Empty is a response with a status and no body, not even a Content-Type.
func Empty(status int) (*Output[EmptyBody], error) {
	return &Output[EmptyBody]{Status: status}, nil
}

// SVGBody is raw SVG, with a string schema and its own response media type.
type SVGBody []byte

func (SVGBody) OASSchema() map[string]any { return map[string]any{"type": "string"} }
func (SVGBody) ContentType(string) string { return "image/svg+xml" }

func SVG(svg string) (*Output[SVGBody], error) {
	cache := "no-cache, no-store, must-revalidate"
	contentType := "image/svg+xml"
	return &Output[SVGBody]{Status: 200, ContentType: &contentType, CacheControl: &cache, Body: SVGBody(svg)}, nil
}

// Stream is a body that the handler writes itself, with its status and headers: a file download.
// The operation documents its response in Operation.Responses.
type Stream func(w http.ResponseWriter)

// ResponseError preserves existing error bodies, including the endpoint-specific
// domain and 2FA responses. Error-model unification belongs to PP-87.
type ResponseError struct {
	Status int
	Body   any
	// Headers are added to the answer, such as the Retry-After of a 429
	Headers http.Header
}

func (e *ResponseError) Error() string                { return http.StatusText(e.Status) }
func (e *ResponseError) GetStatus() int               { return e.Status }
func (e *ResponseError) ContentType(string) string    { return "application/json; charset=utf-8" }
func (e *ResponseError) MarshalJSON() ([]byte, error) { return json.Marshal(e.Body) }
func Fail(status int, body any) error                 { return &ResponseError{Status: status, Body: body} }

// FailWithHeaders is Fail with headers on the answer
func FailWithHeaders(status int, body any, headers http.Header) error {
	return &ResponseError{Status: status, Body: body, Headers: headers}
}

type ErrorBody struct {
	Message string `json:"message"`
	Data    Null   `json:"data"`
}

// Null documents the data field emitted by existing error responses.
type Null struct{}

func (Null) OASSchema() map[string]any    { return map[string]any{"type": "null"} }
func (Null) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// Operation documents a REST operation. The handler's input and output types give the request
// body, the parameters and the success response; the rest is here.
type Operation struct {
	Summary     string
	Description string
	Tags        []string
	// DefaultStatus is the status of the success response, 200 when not set
	DefaultStatus int
	// Security lists the ways to sign in, any one of which opens the operation. Empty, it is public.
	Security []map[string][]string
	// Errors are statuses answered with the usual {message, data: null} body
	Errors []int
	// Responses are the responses that differ from that, by status. An entry for DefaultStatus
	// replaces the success response the output type gives.
	Responses map[string]*Response
	// Options are more documentation, such as parameters a middleware reads
	Options []oasome.RouteOption
}

// Response documents one response of an operation
type Response struct {
	// Description replaces the status text
	Description string
	Headers     []Header
	// body documents the body for a status, nil for a response without one
	body func(status int) oasome.RouteOption
}

// options documents the response as the answer with status
func (r *Response) options(status int) []oasome.RouteOption {
	options := []oasome.RouteOption{oasome.ReturnsEmpty(status)}
	if r.body != nil {
		options[0] = r.body(status)
	}
	for _, header := range r.Headers {
		options = append(options, header.option(status))
	}
	return options
}

// Header documents a header of a response
type Header struct {
	option func(status int) oasome.RouteOption
}

// IntHeader is a header with a whole number
func IntHeader(name, description string) Header {
	return Header{func(status int) oasome.RouteOption {
		return oasome.ResponseHeader[headerInteger](status, name, description)
	}}
}

// headerInteger is the value of an integer header, of any size
type headerInteger int

func (headerInteger) OASSchema() map[string]any { return map[string]any{"type": "integer"} }

// StringHeader is a header with text
func StringHeader(name, description string) Header {
	return Header{func(status int) oasome.RouteOption { return oasome.ResponseHeader[string](status, name, description) }}
}

// JSONResponse is a response with a JSON body of type T
func JSONResponse[T any]() *Response {
	return &Response{body: func(status int) oasome.RouteOption { return oasome.Returns[T](status) }}
}

// ContentResponse is a response with a body of type T in another media type than JSON
func ContentResponse[T any](mediaType string) *Response {
	return &Response{body: func(status int) oasome.RouteOption { return oasome.ReturnsContent[T](status, mediaType) }}
}

// ErrorResponse and ErrorVariants describe the few established responses that
// differ from the normal {message,data:null} envelope.
func ErrorResponse[T any]() *Response { return JSONResponse[T]() }

// ErrorVariants is a response that is either T or the usual error body
func ErrorVariants[T any]() *Response { return JSONResponse[anyOf2[T, ErrorBody]]() }

// ErrorVariants2 is ErrorVariants for an operation that answers with either of two error bodies
// besides the usual one.
func ErrorVariants2[T, U any]() *Response { return JSONResponse[anyOf3[T, U, ErrorBody]]() }

// ErrorVariants3 is ErrorVariants for an operation that answers with any of three error bodies
// besides the usual one.
func ErrorVariants3[T, U, V any]() *Response { return JSONResponse[anyOf4[T, U, V, ErrorBody]]() }

// ErrorVariants4 is ErrorVariants for an operation that answers with any of four error bodies
// besides the usual one.
func ErrorVariants4[T, U, V, W any]() *Response {
	return JSONResponse[anyOf5[T, U, V, W, ErrorBody]]()
}

// anyOf2 to anyOf5 document a body that is any one of their types
type anyOf2[A, B any] struct{}
type anyOf3[A, B, C any] struct{}
type anyOf4[A, B, C, D any] struct{}
type anyOf5[A, B, C, D, E any] struct{}

func (anyOf2[A, B]) OASSchema() map[string]any { return anyOf(oasome.Ref[A](), oasome.Ref[B]()) }
func (anyOf3[A, B, C]) OASSchema() map[string]any {
	return anyOf(oasome.Ref[A](), oasome.Ref[B](), oasome.Ref[C]())
}
func (anyOf4[A, B, C, D]) OASSchema() map[string]any {
	return anyOf(oasome.Ref[A](), oasome.Ref[B](), oasome.Ref[C](), oasome.Ref[D]())
}
func (anyOf5[A, B, C, D, E]) OASSchema() map[string]any {
	return anyOf(oasome.Ref[A](), oasome.Ref[B](), oasome.Ref[C](), oasome.Ref[D](), oasome.Ref[E]())
}

func anyOf(schemas ...any) map[string]any { return map[string]any{"anyOf": schemas} }

// Register adds a REST operation: the handler serves method and path on the router, and the
// operation documents it in the router's OpenAPI document.
func Register[I, T any](router *Router, method, path string, op Operation, handler func(context.Context, *I) (*Output[T], error), middleware ...Middleware) {
	if _, ok := any(new(I)).(inputContract); !ok {
		panic("REST handlers must use httpx.Input")
	}
	if op.DefaultStatus == 0 {
		op.DefaultStatus = http.StatusOK
	}
	doc := router.options(op, documentInput[I](), documentSuccess[T](op.DefaultStatus))
	doc.body = any(new(I)).(inputContract).bodyType()
	router.registry.document(method, join(router.prefix, path), doc)
	router.Handle(method, path, serve(op.DefaultStatus, handler), middleware...)
}

// serve is the HTTP handler of a typed handler: it calls it with the request, and writes its
// output or its error
func serve[I, T any](defaultStatus int, handler func(context.Context, *I) (*Output[T], error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input := new(I)
		any(input).(inputContract).setRequest(r)
		output, err := handler(r.Context(), input)
		if err != nil {
			writeError(w, err)
			return
		}
		if output == nil {
			w.WriteHeader(defaultStatus)
			return
		}
		output.write(w, defaultStatus)
	}
}

// write sends the output: its headers, its status and its body
func (o *Output[T]) write(w http.ResponseWriter, defaultStatus int) {
	setHeader(w, "Content-Type", o.ContentType)
	setHeader(w, "Cache-Control", o.CacheControl)
	status := o.Status
	if status == 0 {
		status = defaultStatus
	}
	switch body := any(o.Body).(type) {
	case Stream:
		body(w)
	case []byte:
		w.WriteHeader(status)
		_, _ = w.Write(body)
	default:
		writeBody(w, status, o.Body)
	}
}

func setHeader(w http.ResponseWriter, name string, value *string) {
	if value != nil {
		w.Header().Set(name, *value)
	}
}

// writeError answers with a handler's error. A ResponseError gives its status, headers and body;
// any other error is a 500 that says nothing of its cause.
func writeError(w http.ResponseWriter, err error) {
	var answer *ResponseError
	if !errors.As(err, &answer) {
		answer = &ResponseError{Status: http.StatusInternalServerError, Body: ErrorBody{Message: "Internal server error"}}
	}
	for name, values := range answer.Headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	writeBody(w, answer.Status, answer)
}

// contentTyped is a body with its own media type
type contentTyped interface{ ContentType(string) string }

// writeBody writes body as JSON, or as it is when it is SVG. The Content-Type is the body's own,
// or JSON, unless the handler set one. A 204 or a 304 has no body.
func writeBody(w http.ResponseWriter, status int, body any) {
	if w.Header().Get("Content-Type") == "" {
		contentType := "application/json"
		if typed, ok := body.(contentTyped); ok {
			contentType = typed.ContentType(contentType)
		}
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return
	}
	if svg, ok := body.(SVGBody); ok {
		_, _ = w.Write(svg)
		return
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(b)
}

// documentInput documents the request of a handler with input I
func documentInput[I any]() []oasome.RouteOption {
	return any(new(I)).(inputContract).document()
}

// documentSuccess documents the success response of a handler with output body T: none for
// EmptyBody and for a 204 or a 304, SVG with its Cache-Control header, and JSON otherwise. A
// Stream documents nothing: its operation lists the response.
func documentSuccess[T any](status int) *Response {
	switch reflect.TypeFor[T]() {
	case reflect.TypeFor[Stream]():
		return nil
	case reflect.TypeFor[SVGBody]():
		response := ContentResponse[SVGBody]("image/svg+xml")
		response.Headers = []Header{StringHeader("Cache-Control", "")}
		return response
	case reflect.TypeFor[EmptyBody]():
		return &Response{}
	}
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return &Response{}
	}
	return JSONResponse[T]()
}

// options are the documentation of an operation on this router: the router's own, the request,
// the responses, the security and the operation's text
func (r *Router) options(op Operation, request []oasome.RouteOption, success *Response) operationDoc {
	options := append(append([]oasome.RouteOption(nil), request...), r.parameters...)
	options = append(options, op.Options...)
	responses := r.responses(op, success)
	for status, response := range responses {
		options = append(options, response.options(status)...)
	}
	for _, alternative := range op.Security {
		for scheme := range alternative {
			options = append(options, oasome.Security(scheme))
		}
	}
	options = append(options, oasome.Summary(op.Summary), oasome.Description(op.Description), oasome.Tags(op.Tags...))
	return operationDoc{options: options, responses: responses}
}

// responses are the responses of an operation by status: the success response, the operation's
// own, and the usual error body for each of its errors and the router's
func (r *Router) responses(op Operation, success *Response) map[int]*Response {
	responses := map[int]*Response{}
	if success != nil {
		responses[op.DefaultStatus] = success
	}
	for code, response := range op.Responses {
		status, err := strconv.Atoi(code)
		if err != nil {
			panic("httpx: response status " + code + " is not a number")
		}
		responses[status] = response
	}
	for _, status := range append(append([]int(nil), op.Errors...), r.errors...) {
		if responses[status] == nil {
			responses[status] = errorResponse(status)
		}
	}
	return responses
}

// errorResponse is the usual error body, with Retry-After on a 429
func errorResponse(status int) *Response {
	response := JSONResponse[ErrorBody]()
	if status == http.StatusTooManyRequests {
		response.Headers = []Header{retryAfterHeader}
	}
	return response
}
