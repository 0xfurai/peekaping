package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// singleConfig is cmd/peekaping's config, with its own SQLite database and no Redis
func singleConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		DBType:                "sqlite",
		DBName:                filepath.Join(t.TempDir(), "peekaping.db"),
		Mode:                  "test",
		LogLevel:              "fatal",
		Timezone:              "UTC",
		CheckConcurrency:      1,
		BruteforceMaxAttempts: 20,
		BruteforceWindow:      time.Minute,
		BruteforceLockout:     time.Minute,
		ClientURL:             "http://localhost:3000",
		ServiceName:           "peekaping:apptest",
	}
}

func TestRegisterSingle_ResolvesWhatCmdPeekapingRuns(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	// What cmd/peekaping invokes: everything cmd/api does, the stats event handlers the
	// ingester registers, and the scheduler with its watchdog
	require.NoError(t, container.Invoke(func(
		_ heartbeat.Service,
		_ auth.Repository,
		_ setting.Service,
		_ stats.Service,
		_ notification_sent_history.Service,
		_ monitor_tls_info.Service,
		_ cleanup.RunLock,
		_ *zap.SugaredLogger,
		_ *notification_channel.NotificationEventListener,
		_ *monitor.MonitorEventListener,
		_ monitor.Service,
		_ dbtx.Transactor,
		_ *metrics.Collector,
		_ *internal.Server,
		_ *websocket.Server,
		_ *scheduler.Scheduler,
		_ *stale.Watchdog,
		_ stale.Store,
		bus events.EventBus,
		db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
	}))
}

// The one process keeps the duplicate window of the results route in its memory: no Redis is needed
func TestRegisterSingle_ProvidesTheDuplicateWindowInMemory(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(window protocol.Window, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		outcome, err := window.Claim(context.Background(), "probe-1", "result-1", time.Now())
		require.NoError(t, err)
		assert.Equal(t, protocol.ClaimWon, outcome)
		outcome, err = window.Claim(context.Background(), "probe-1", "result-1", time.Now())
		require.NoError(t, err)
		assert.Equal(t, protocol.ClaimPending, outcome)
	}))
}

// The sweep that sets silent probes offline runs in the one process, on the in-memory event bus
func TestRegisterSingle_ResolvesTheProbeSweep(t *testing.T) {
	container := dig.New()
	cfg := singleConfig(t)
	cfg.ProbeOfflineAfter = 5 * time.Minute
	require.NoError(t, RegisterSingle(container, cfg))

	require.NoError(t, container.Invoke(func(sweep *probe.Sweep, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		assert.NotNil(t, sweep)
	}))
}

// The local probe never counts as offline for the watchdog, so nothing changes for an install that
// has no remote probe. A monitor of a remote probe that is offline is left to the probe's own
// notification.
func TestRegisterSingle_TheWatchdogSkipsOnlyTheMonitorsOfAnOfflineRemoteProbe(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(probeOffline stale.ProbeOfflineFunc, monitors monitor.Service, probes probe.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))

		assertWatchdogSkipsOnlyOfflineRemote(t, probeOffline, monitors, probes)
	}))
}

// The one process runs every hour's cleanup, so its run lock needs no Redis and always grants it
func TestRegisterSingle_ProvidesARunLockThatGrantsEveryHour(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(lock cleanup.RunLock) {
		hour := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for _, h := range []time.Time{hour, hour, hour.Add(time.Hour)} {
			claimed, err := lock.Claim(context.Background(), h)
			require.NoError(t, err)
			assert.True(t, claimed, "claim of %s", h)
		}
	}))
}

func TestRegisterSingle_ProvidesNoRedisAndNoQueue(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	assert.Error(t, container.Invoke(func(*redis.Client) {}), "single mode has no Redis")
	assert.Error(t, container.Invoke(func(*asynq.Client) {}), "single mode has no asynq client")
	assert.Error(t, container.Invoke(func(*asynq.Server) {}), "single mode has no asynq server")
	assert.Error(t, container.Invoke(func(queue.Service) {}), "single mode has no queue")
}

// The usage ping reports the mode its registration gives it, not the MODE setting, and in single
// mode the one process always sends the day's ping, without Redis.
func TestRegisterSingle_RunsTheUsagePingInSingleModeWithoutRedis(t *testing.T) {
	container := dig.New()
	cfg := singleConfig(t)
	cfg.Mode = "prod"
	require.NoError(t, RegisterSingle(container, cfg))

	require.NoError(t, container.Invoke(func(
		mode telemetry.Mode,
		claimer telemetry.Claimer,
		_ *telemetry.Sender,
		bus events.EventBus,
		db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		assert.Equal(t, telemetry.ModeSingle, mode)
		assert.Equal(t, telemetry.AlwaysClaimer{}, claimer)
	}))
}

// The scheduler wraps its ingester.Service in ingest.NewInOrderService, which returns a service
// that is already in order as it is. So the push URL's service, the one the container provides,
// is the very one the scheduler ingests through, and a monitor's pushes wait for its checks.
func TestRegisterSingle_ProvidesTheCORSOriginsFromTheConfig(t *testing.T) {
	for name, tc := range map[string]struct{ origins, clientURL, allowed, denied string }{
		"the list": {"https://a.example,https://b.example", "https://peek.example", "https://b.example", "https://peek.example"},
		"the origin of CLIENT_URL when the list is unset": {"", "https://peek.example/status", "https://peek.example", "https://a.example"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := singleConfig(t)
			cfg.CORSOrigins = tc.origins
			cfg.ClientURL = tc.clientURL
			container := dig.New()
			require.NoError(t, RegisterSingle(container, cfg))

			require.NoError(t, container.Invoke(func(origins *httpx.CORSOrigins) {
				assert.Equal(t, tc.allowed, origins.AllowOrigin(tc.allowed))
				assert.Empty(t, origins.AllowOrigin(tc.denied))
			}))
		})
	}
}

func TestRegisterSingle_RefusesAnInvalidCORSOrigin(t *testing.T) {
	cfg := singleConfig(t)
	cfg.CORSOrigins = "https://peek.example,example.com"
	container := dig.New()
	require.NoError(t, RegisterSingle(container, cfg))

	err := container.Invoke(func(*httpx.CORSOrigins) {})

	assert.ErrorContains(t, err, `invalid CORS_ORIGINS entry "example.com"`)
}

func TestRegisterSingle_IngestsThePushURLThroughTheSchedulersInOrderService(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(svc ingester.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		assert.Same(t, svc, ingest.NewInOrderService(svc), "the provided ingester.Service is in order")
	}))
}

// The controller validates and sends with the providers the container gives it, so a Test
// notification works with only the controller resolved, without the listener built first
func TestRegisterSingle_TestsAChannelWithoutTheNotificationListenerBuilt(t *testing.T) {
	var webhookCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
	}))
	t.Cleanup(target.Close)
	webhookConfig, err := json.Marshal(map[string]string{"webhook_url": target.URL, "webhook_content_type": "json"})
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"name": "Alerts", "type": "webhook", "config": string(webhookConfig)})
	require.NoError(t, err)

	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(controller *notification_channel.Controller, db *bun.DB) {
		closeDB(t, db)
		router := httpx.NewRouter()
		httpx.Register(router, "POST", "/test", huma.Operation{Errors: []int{400, 404, 500}}, controller.Test)

		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, int32(1), webhookCalls.Load(), "requests the webhook got")
	}))
}
