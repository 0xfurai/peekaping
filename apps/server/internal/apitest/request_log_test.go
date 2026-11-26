package apitest

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func withRequestID(id string) credentials {
	return func(req *http.Request) { req.Header.Set("X-Request-ID", id) }
}

// The request log is on in every mode, not only in dev
func TestRequestLog_WritesOneLineForEachRequestInProdMode(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, func(cfg *config.Config) { cfg.Mode = "prod" })
	a.registerAdmin(t)
	const pushToken = "secret-push-token"
	a.createMonitor(t, "Checkout", pushToken)

	for _, tc := range []struct {
		name, path, route string
		status            int
		level             zapcore.Level
	}{
		{"an API route", "/api/v1/version", "/api/v1/version", http.StatusOK, zap.InfoLevel},
		{"the push URL", "/api/v1/push/" + pushToken + "?status=up&msg=OK", "/api/v1/push/{token}", http.StatusOK, zap.InfoLevel},
		{"the health endpoint", "/api/v1/health", "/api/v1/health", http.StatusOK, zap.DebugLevel},
		{"no route", "/nothing/here?token=" + pushToken, "", http.StatusNotFound, zap.InfoLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.TakeAll()
			w := a.request(t, http.MethodGet, tc.path, nil, withRequestID("client-request-7"))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, "client-request-7", w.Header().Get("X-Request-ID"))

			lines := logs.FilterMessage("HTTP request").All()
			require.Len(t, lines, 1)
			assert.Equal(t, tc.level, lines[0].Level)
			entry := lines[0].ContextMap()
			assert.ElementsMatch(t, []string{"request_id", "method", "route", "status", "client_ip", "size", "duration"}, slices.Collect(maps.Keys(entry)))
			assert.Equal(t, "client-request-7", entry["request_id"])
			assert.Equal(t, tc.route, entry["route"])
			assert.NotContains(t, fmt.Sprint(entry), pushToken)
		})
	}

	t.Run("a request without an ID gets one", func(t *testing.T) {
		logs.TakeAll()
		w := a.request(t, http.MethodGet, "/api/v1/version", nil)
		id := w.Header().Get("X-Request-ID")
		require.NotEmpty(t, id)
		lines := logs.FilterMessage("HTTP request").All()
		require.Len(t, lines, 1)
		assert.Equal(t, id, lines[0].ContextMap()["request_id"])
	})
}
