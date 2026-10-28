package httpx

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type Route struct{ Method, Path string }
type registry struct {
	api              huma.API
	mux              *http.ServeMux
	routes           []Route
	middleware       []Middleware
	catchAllPrefixes []string
}

// Router groups registration on a ServeMux and records the routes for auditing.
// All registered handlers and middleware use standard net/http interfaces.
type Router struct {
	registry   *registry
	prefix     string
	middleware []Middleware
	// parameters are documented on every operation registered after they are added
	parameters []*huma.Param
}

func NewRouter(middleware ...Middleware) *Router {
	mux := http.NewServeMux()
	// The fallback preserves the existing 404 response for unsupported methods.
	mux.HandleFunc("/", NotFound)
	return &Router{registry: &registry{mux: mux, middleware: middleware}}
}
func (r *Router) Group(prefix string) *Router {
	return &Router{registry: r.registry, prefix: join(r.prefix, prefix), middleware: append([]Middleware(nil), r.middleware...), parameters: append([]*huma.Param(nil), r.parameters...)}
}

// Document adds parameters to the API reference of every operation that is registered on this
// router, or on a group made from it, after the call. It documents what a middleware reads, such
// as a header, once for the group of routes behind that middleware and not on each operation.
func (r *Router) Document(parameters ...*huma.Param) {
	r.parameters = append(r.parameters, parameters...)
}
func (r *Router) Use(middleware ...Middleware) { r.middleware = append(r.middleware, middleware...) }
func join(prefix, suffix string) string {
	if suffix == "" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(suffix, "/")
}
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
func (r *Router) Handle(method, path string, handler http.HandlerFunc, middleware ...Middleware) {
	pattern := join(r.prefix, path)
	wrapped := Chain(handler, append(append([]Middleware(nil), r.middleware...), middleware...)...)
	r.registry.routes = append(r.registry.routes, Route{method, pattern})
	if strings.HasSuffix(pattern, "...}") {
		prefix := pattern[:strings.LastIndex(pattern, "/{")+1]
		if !slices.Contains(r.registry.catchAllPrefixes, prefix) {
			r.registry.catchAllPrefixes = append(r.registry.catchAllPrefixes, prefix)
			// Exact bare-prefix fallbacks suppress subtree redirects in every Go
			// version. A later explicit method-specific route still takes precedence.
			if prefix != "/" {
				r.registry.mux.HandleFunc(strings.TrimSuffix(prefix, "/"), NotFound)
			}
		}
	}
	muxPattern := pattern
	if strings.HasSuffix(muxPattern, "/") {
		muxPattern += "{$}"
	}
	r.registry.mux.Handle(method+" "+muxPattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// ServeMux GET also accepts HEAD; the push URL must never write on HEAD.
		if req.Method != method {
			NotFound(w, req)
			return
		}
		setRoute(req.Context(), pattern)
		wrapped.ServeHTTP(w, req)
	}))
}
func (r *Router) Routes() []Route { return append([]Route(nil), r.registry.routes...) }
func NotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("404 page not found"))
}
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Gin matched the decoded path without cleaning it. Encode literal dot
		// segments for ServeMux, and discard RawPath so encoded slashes remain
		// separators. Do not redirect malformed or trailing-slash paths.
		catchAllPrefix := ""
		for _, prefix := range r.registry.catchAllPrefixes {
			if strings.HasPrefix(req.URL.Path, prefix) {
				catchAllPrefix = prefix
				break
			}
		}
		if strings.Contains(req.URL.Path, "//") && catchAllPrefix == "" {
			NotFound(w, req)
			return
		}
		parts := strings.Split(req.URL.Path, "/")
		for i, part := range parts {
			if part == "." || part == ".." {
				parts[i] = strings.ReplaceAll(part, ".", "%2E")
			} else {
				parts[i] = url.PathEscape(part)
			}
		}
		copyReq := req.Clone(req.Context())
		copyReq.URL.RawPath = strings.Join(parts, "/")
		if catchAllPrefix != "" {
			tail := strings.TrimPrefix(req.URL.Path, catchAllPrefix)
			escapedTail := url.PathEscape(tail)
			if tail == "." || tail == ".." {
				escapedTail = strings.ReplaceAll(tail, ".", "%2E")
			}
			copyReq.URL.RawPath = catchAllPrefix + escapedTail
		}
		r.registry.mux.ServeHTTP(w, copyReq)
	})
	Chain(handler, r.registry.middleware...).ServeHTTP(w, req)
}
