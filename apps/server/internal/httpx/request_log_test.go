package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestID(t *testing.T) {
	for _, tc := range []struct {
		name, incoming string
		echoed         bool
	}{
		{"a valid ID is echoed", "client-id_1.2", true},
		{"64 characters are the longest ID echoed", strings.Repeat("a", 64), true},
		{"an over-long ID is replaced", strings.Repeat("a", 65), false},
		{"an ID with a space is replaced", "two words", false},
		{"an ID with a quote is replaced", `id"x`, false},
		{"an ID with a slash is replaced", "a/b", false},
		{"a non-ASCII ID is replaced", "idé", false},
		{"a missing ID is generated", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := httpx.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			r := httptest.NewRequest("GET", "/", nil)
			if tc.incoming != "" {
				r.Header.Set("X-Request-ID", tc.incoming)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			got := w.Header().Get("X-Request-ID")
			if tc.echoed {
				assert.Equal(t, tc.incoming, got)
				return
			}
			assert.NotEqual(t, tc.incoming, got)
			assert.Regexp(t, `^[A-Za-z0-9._-]{1,64}$`, got)
		})
	}

	t.Run("generated IDs differ", func(t *testing.T) {
		handler := httpx.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		first, second := httptest.NewRecorder(), httptest.NewRecorder()
		handler.ServeHTTP(first, httptest.NewRequest("GET", "/", nil))
		handler.ServeHTTP(second, httptest.NewRequest("GET", "/", nil))
		assert.NotEqual(t, first.Header().Get("X-Request-ID"), second.Header().Get("X-Request-ID"))
	})
}

func TestLogFromContext(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	base := zap.New(core).Sugar()

	httpx.Log(context.Background(), base).Infow("outside a request")
	handler := httpx.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Log(r.Context(), base).Infow("inside a request", "key", "value")
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "client-id")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	outside := logs.FilterMessage("outside a request").All()[0].ContextMap()
	assert.NotContains(t, outside, "request_id")
	inside := logs.FilterMessage("inside a request").All()[0].ContextMap()
	assert.Equal(t, "client-id", inside["request_id"])
	assert.Equal(t, "value", inside["key"])
}
