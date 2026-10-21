package app

import (
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"

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
		_ *zap.SugaredLogger,
		_ *notification_channel.NotificationEventListener,
		_ *monitor.MonitorEventListener,
		_ monitor.Service,
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

// The scheduler wraps its ingester.Service in ingester.NewInOrderService, which returns a service
// that is already in order as it is. So the push URL's service, the one the container provides,
// is the very one the scheduler ingests through, and a monitor's pushes wait for its checks.
func TestRegisterSingle_IngestsThePushURLThroughTheSchedulersInOrderService(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(svc ingester.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		assert.Same(t, svc, ingester.NewInOrderService(svc), "the provided ingester.Service is in order")
	}))
}
