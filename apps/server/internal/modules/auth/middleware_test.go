package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/shared"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// caller is what a handler behind the middleware saw.
type caller struct {
	ran                     bool
	userID, email, authType any
}

// serve sends a request with the given Authorization header through the
// middleware to a handler, and returns the response and what the handler saw.
func serve(h *harness, authorization string) (*httptest.ResponseRecorder, caller) {
	var seen caller
	router := gin.New()
	router.GET("/protected", auth.NewMiddlewareProvider(h.tokens).Auth(), func(c *gin.Context) {
		seen = caller{true, c.Value("userId"), c.Value("email"), c.Value("authType")}
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, seen
}

func TestMiddleware_LetsAValidAccessTokenThrough(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	for name, authorization := range map[string]string{
		"bearer token": "Bearer " + session.AccessToken,
		"bare token":   session.AccessToken,
	} {
		t.Run(name, func(t *testing.T) {
			w, seen := serve(h, authorization)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, caller{true, session.User.ID, adminEmail, "jwt"}, seen)
		})
	}
}

func TestMiddleware_RejectsMissingOrBadTokens(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	accessSecret := h.secret(t, shared.AccessTokenSecretKey)
	claims := func(tokenType string, expiresAt time.Time) auth.Claims {
		return auth.Claims{UserID: session.User.ID, Email: adminEmail, Type: tokenType, RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		}}
	}

	requests := []struct {
		name, authorization, message string
	}{
		{"no header", "", "Authorization header is required"},
		{"two tokens", "Bearer a b", "Invalid authorization header format"},
		{"another scheme", "Basic YWRtaW46cGFzcw==", "Invalid authorization header format"},
		{"not a token", "Bearer not-a-token", "Invalid or expired token"},
		{"expired", "Bearer " + sign(t, accessSecret, claims("access", time.Now().Add(-time.Minute))), "Invalid or expired token"},
		{"another secret", "Bearer " + sign(t, "another-secret", claims("access", time.Now().Add(time.Hour))), "Invalid or expired token"},
		{"refresh token", "Bearer " + session.RefreshToken, "Invalid or expired token"},
		{"refresh type signed as access", "Bearer " + sign(t, accessSecret, claims("refresh", time.Now().Add(time.Hour))), "Invalid token type"},
	}
	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			w, seen := serve(h, r.authorization)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.False(t, seen.ran, "the handler doesn't run")
			var body struct {
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
			assert.Equal(t, r.message, body.Message)
		})
	}
}
