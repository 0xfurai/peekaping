package httpx

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	requestIDHeader = "X-Request-ID"
	maxRequestIDLen = 64
)

// requestInfo is what the request's context carries for logging. route is a slot the router
// fills in: it hands ServeMux a clone of the request, so the middleware around the router can't
// read the matched pattern from the request, but the clone shares the context.
type requestInfo struct {
	id    string
	route string
	// logged is set by the first Logger the request reaches, which logs it once it is done
	logged bool
}

type requestInfoKey struct{}

func requestInfoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*requestInfo)
	return info
}

// setRoute records the pattern of the route serving the request, such as /api/v1/push/{token}.
func setRoute(ctx context.Context, pattern string) {
	if info := requestInfoFrom(ctx); info != nil {
		info.route = pattern
	}
}

// RequestID gives each request an ID and sets it as the X-Request-ID header of the response. It
// keeps the request's own X-Request-ID when that is up to 64 characters of A-Z, a-z, 0-9, '.',
// '_' and '-', and generates one otherwise. A request that already carries an ID, from a RequestID
// further out, keeps it.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestInfoFrom(r.Context()) != nil {
			next.ServeHTTP(w, r)
			return
		}
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestInfoKey{}, &requestInfo{id: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Log returns base with the request's ID as its request_id field, or base itself when ctx isn't
// a request's context that RequestID prepared.
func Log(ctx context.Context, base *zap.SugaredLogger) *zap.SugaredLogger {
	if info := requestInfoFrom(ctx); info != nil {
		return base.With("request_id", info.id)
	}
	return base
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range []byte(id) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
