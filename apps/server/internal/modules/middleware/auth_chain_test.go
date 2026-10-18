package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/modules/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// authenticator stands in for the JWT or API key middleware: it lets every
// request through and marks it with its own auth type.
type authenticator string

func (a authenticator) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authType", string(a))
		c.Next()
	}
}

// caller is what a handler behind the auth chain saw.
type caller struct {
	ran      bool
	authType any
}

// serve sends a request with the given headers through the auth chain to a
// handler, and returns the response and what the handler saw.
func serve(headers map[string]string) (*httptest.ResponseRecorder, caller) {
	chain := middleware.NewAuthChain(authenticator("jwt"), authenticator("api_key"), zap.NewNop().Sugar())
	var seen caller
	router := gin.New()
	router.GET("/protected", chain.AllAuth(), func(c *gin.Context) {
		seen = caller{true, c.Value("authType")}
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, seen
}

func TestAuthChain_RoutesByCredentialHeader(t *testing.T) {
	requests := []struct {
		name     string
		headers  map[string]string
		authType string
	}{
		{"API key", map[string]string{"X-API-Key": "pk_key"}, "api_key"},
		{"JWT", map[string]string{"Authorization": "Bearer token"}, "jwt"},
		{"API key wins over JWT", map[string]string{"X-API-Key": "pk_key", "Authorization": "Bearer token"}, "api_key"},
		{"empty API key falls back to JWT", map[string]string{"X-API-Key": "", "Authorization": "Bearer token"}, "jwt"},
	}
	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			w, seen := serve(r.headers)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, caller{true, r.authType}, seen)
		})
	}
}

func TestAuthChain_RejectsRequestsWithoutCredentials(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"no headers":          {},
		"empty API key":       {"X-API-Key": ""},
		"empty Authorization": {"Authorization": ""},
	} {
		t.Run(name, func(t *testing.T) {
			w, seen := serve(headers)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.False(t, seen.ran, "the handler doesn't run")
			assert.JSONEq(t, `{"message":"Authentication required: provide either X-API-Key header or Authorization header","data":null}`, w.Body.String())
		})
	}
}
