// Package httpx contains shared HTTP response and request helpers.
package httpx

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

type Middleware func(http.Handler) http.Handler

// JSON retains the API's compact JSON responses, including HTML escaping.
func JSON(w http.ResponseWriter, status int, value any) {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return
	}
	_, _ = w.Write(b)
}
func Text(w http.ResponseWriter, status int, value string) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(value))
}

func DecodeJSON(r *http.Request, value any) error { return json.NewDecoder(r.Body).Decode(value) }
func DefaultQuery(r *http.Request, key, fallback string) string {
	if values, ok := r.URL.Query()[key]; ok && len(values) > 0 {
		return values[0]
	}
	return fallback
}

type identityKey string

// WithValue stores HTTP authentication metadata under a private key type.
func WithValue(r *http.Request, key string, value any) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), identityKey(key), value))
}
func Value(ctx context.Context, key string) any { return ctx.Value(identityKey(key)) }

// ClientIP retains the existing forwarded-header policy. Trusted proxy policy
// and account-level lockout hardening are tracked separately in PP-50.
func ClientIP(r *http.Request) string {
	remote, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return ""
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		values := strings.Split(r.Header.Get(header), ",")
		valid := true
		for _, value := range values {
			if net.ParseIP(strings.TrimSpace(value)) == nil {
				valid = false
				break
			}
		}
		if valid {
			return strings.TrimSpace(values[0])
		}
	}
	return remote
}

func Lookup(ctx context.Context, key string) (any, bool) { v := Value(ctx, key); return v, v != nil }
