package httpx_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

func TestRequestLogUsesRoutePattern(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	router := httpx.NewRouter(httpx.RequestID, httpx.Logger(zap.New(core).Sugar(), nil))
	ok := func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "ok") }
	router.Group("/api/v1").Handle("GET", "/push/{token}", ok)
	router.Handle("GET", "/socket.io/{f...}", ok)

	for _, tc := range []struct {
		name, path, route string
		status            int
		secrets           []string
	}{
		{"a path secret", "/api/v1/push/s3cr3t-token?status=up&msg=hi", "/api/v1/push/{token}", 200, []string{"s3cr3t-token", "status=up", "msg=hi"}},
		{"a query secret", "/socket.io/?EIO=4&token=s3cr3t-jwt", "/socket.io/{f...}", 200, []string{"s3cr3t-jwt", "EIO=4", "token="}},
		{"no matching route", "/nothing/s3cr3t-path?q=s3cr3t-query", "", 404, []string{"s3cr3t-path", "s3cr3t-query"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			require.Equal(t, tc.status, w.Code)

			all := logs.TakeAll()
			require.Len(t, all, 1)
			assert.Equal(t, "HTTP request", all[0].Message)
			entry := all[0].ContextMap()
			assert.ElementsMatch(t, []string{"request_id", "method", "route", "status", "client_ip", "size", "duration"}, slices.Collect(maps.Keys(entry)))
			assert.Equal(t, w.Header().Get("X-Request-ID"), entry["request_id"])
			assert.Equal(t, "GET", entry["method"])
			assert.Equal(t, tc.route, entry["route"])
			assert.EqualValues(t, tc.status, entry["status"])
			assert.Equal(t, "192.0.2.1", entry["client_ip"])
			assert.EqualValues(t, w.Body.Len(), entry["size"])
			for _, secret := range tc.secrets {
				assert.NotContains(t, fmt.Sprint(all[0].Message, entry), secret)
			}
		})
	}
}

func TestRequestLogLevel(t *testing.T) {
	newRouter := func(level zapcore.Level) (*httpx.Router, *observer.ObservedLogs) {
		core, logs := observer.New(level)
		router := httpx.NewRouter(httpx.RequestID, httpx.Logger(zap.New(core).Sugar(), nil))
		ok := func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "ok") }
		router.Handle("GET", "/health", ok)
		router.Handle("GET", "/metrics", ok)
		router.Group("/api/v1").Handle("GET", "/health", ok)
		router.Group("/api/v1").Handle("GET", "/monitors", ok)
		router.Group("/probe/v1").Handle("GET", "/assignments", ok)
		router.Group("/probe/v1").Handle("GET", "/hello", ok)
		return router, logs
	}

	for _, tc := range []struct {
		path  string
		level zapcore.Level
	}{
		{"/health", zap.DebugLevel},
		{"/api/v1/health", zap.DebugLevel},
		{"/metrics", zap.DebugLevel},
		{"/probe/v1/assignments?version=v1", zap.DebugLevel},
		{"/probe/v1/hello", zap.InfoLevel},
		{"/api/v1/monitors", zap.InfoLevel},
		{"/api/v1/monitors?next=/health", zap.InfoLevel},
		{"/health/extra", zap.InfoLevel},
		{"/nothing", zap.InfoLevel},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router, logs := newRouter(zap.DebugLevel)
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", tc.path, nil))
			all := logs.TakeAll()
			require.Len(t, all, 1)
			assert.Equal(t, tc.level, all[0].Level)
		})
	}

	t.Run("the default log level skips the health and metrics endpoints", func(t *testing.T) {
		router, logs := newRouter(zap.InfoLevel)
		for _, path := range []string{"/health", "/api/v1/health", "/metrics", "/api/v1/monitors"} {
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		}
		assert.Equal(t, 1, logs.Len())
	})
}

// A request that no route served, such as the one the web gateway answers itself, has no route to
// go by, so its path decides the level.
func TestRequestLogLevelOfARequestWithoutARoute(t *testing.T) {
	for _, tc := range []struct {
		target string
		level  zapcore.Level
	}{
		{"/health", zap.DebugLevel},
		{"/health?probe=1", zap.DebugLevel},
		{"/api/v1/health", zap.DebugLevel},
		{"/metrics", zap.DebugLevel},
		{"/health/extra", zap.InfoLevel},
		{"/assets/index.js", zap.InfoLevel},
		{"/monitors?next=/health", zap.InfoLevel},
	} {
		t.Run(tc.target, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			handler := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "ok") }),
				httpx.RequestID, httpx.Logger(zap.New(core).Sugar(), nil))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", tc.target, nil))
			all := logs.TakeAll()
			require.Len(t, all, 1)
			assert.Equal(t, tc.level, all[0].Level)
			assert.Equal(t, "", all[0].ContextMap()["route"])
		})
	}
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

// A request can pass through two Loggers, as in single mode with WEB_DIR, where the gateway wraps
// the router. The outer one logs the request, once.
func TestLoggerLogsARequestOnceWhenAnOuterLoggerHandlesIt(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	base := zap.New(core).Sugar()
	router := httpx.NewRouter(httpx.RequestID, httpx.Logger(base, nil))
	router.Group("/api/v1").Handle("GET", "/push/{token}", func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "ok") })
	outer := httpx.Chain(router, httpx.RequestID, httpx.Logger(base, nil))

	w := httptest.NewRecorder()
	outer.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/push/s3cr3t-token?status=up", nil))

	all := logs.TakeAll()
	require.Len(t, all, 1)
	entry := all[0].ContextMap()
	assert.Equal(t, "HTTP request", all[0].Message)
	assert.Equal(t, w.Header().Get("X-Request-ID"), entry["request_id"])
	assert.Equal(t, "/api/v1/push/{token}", entry["route"], "the router still fills in the route")
	assert.EqualValues(t, 200, entry["status"])
	assert.EqualValues(t, 2, entry["size"])
}

// A request can pass through two RequestID middlewares, as in single mode with WEB_DIR, where the
// gateway wraps the router. The first one's ID stays the request's ID.
func TestRequestIDKeepsTheIDOfAnOuterRequestID(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	base := zap.New(core).Sugar()
	handler := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Log(r.Context(), base).Infow("inside")
	}), httpx.RequestID, httpx.Logger(base, nil), httpx.RequestID)

	for _, incoming := range []string{"", "client-id", "two words"} {
		t.Run("incoming ID "+incoming, func(t *testing.T) {
			logs.TakeAll()
			r := httptest.NewRequest("GET", "/", nil)
			if incoming != "" {
				r.Header.Set("X-Request-ID", incoming)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			require.Len(t, w.Header().Values("X-Request-ID"), 1)
			id := w.Header().Get("X-Request-ID")
			if incoming == "client-id" {
				assert.Equal(t, incoming, id)
			}
			assert.Equal(t, id, logs.FilterMessage("inside").All()[0].ContextMap()["request_id"])
			assert.Equal(t, id, logs.FilterMessage("HTTP request").All()[0].ContextMap()["request_id"])
		})
	}
}
