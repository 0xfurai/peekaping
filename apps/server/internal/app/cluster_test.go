package app

import (
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/worker"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// clusterConfig is a cluster binary's config, with its own SQLite database and in-process Redis
func clusterConfig(t *testing.T) *config.Config {
	t.Helper()
	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	return &config.Config{
		DBType:                "sqlite",
		DBName:                filepath.Join(t.TempDir(), "peekaping.db"),
		Mode:                  "test",
		LogLevel:              "fatal",
		Timezone:              "UTC",
		RedisHost:             redisHost,
		RedisPort:             redisPort,
		QueueConcurrency:      1,
		ProducerConcurrency:   1,
		BruteforceMaxAttempts: 20,
		BruteforceWindow:      time.Minute,
		BruteforceLockout:     time.Minute,
		ClientURL:             "http://localhost:3000",
		ServiceName:           "peekaping:apptest",
	}
}

// closeEventBus closes the event bus when the test ends
func closeEventBus(t *testing.T, bus events.EventBus) {
	t.Cleanup(func() { assert.NoError(t, bus.Close(context.Background())) })
}

func closeDB(t *testing.T, db *bun.DB) {
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
}

func TestRegisterClusterAPI_ResolvesWhatCmdAPIRuns(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	// What cmd/api invokes: the cleanup cron, the settings, the listeners, the status repair,
	// the metrics collector, the server, and the database it closes on shutdown
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
		_ *asynq.Server,
		_ *notification_channel.QueueConsumer,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
	}))
}

func TestRegisterClusterProducer_ResolvesWhatCmdProducerRuns(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterProducer(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		_ *producer.Producer,
		_ *producer.EventListener,
		_ *zap.SugaredLogger,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
	}))
}

func TestRegisterClusterWorker_ResolvesWhatCmdWorkerRunsWithoutADatabase(t *testing.T) {
	// cmd/worker reads no database settings
	cfg := clusterConfig(t)
	cfg.DBType, cfg.DBName = "", ""
	container := dig.New()
	require.NoError(t, RegisterClusterWorker(container, cfg))

	require.NoError(t, container.Invoke(func(
		_ *worker.Worker,
		_ *zap.SugaredLogger,
		bus events.EventBus,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
	}))
	assert.Error(t, container.Invoke(func(*bun.DB) {}), "the worker has no database")
}

func TestRegisterClusterIngester_ResolvesWhatCmdIngesterRuns(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterIngester(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		_ stats.Service,
		_ *ingester.Ingester,
		_ *zap.SugaredLogger,
		bus events.EventBus,
		db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
	}))
}

// Redis pub/sub hands an event to every api replica, so the topics the notification listener
// sends from go through the notification queue, which gives each event to one replica. The
// ingester publishes two of them and the producer's watchdog the other two, and every other
// topic stays on pub/sub for all the api replicas, so each cluster binary's bus must route
// them alike.
func TestRegisterCluster_EveryBinarysEventBusQueuesTheNotificationTopics(t *testing.T) {
	registers := map[string]func(*dig.Container, *config.Config) error{
		"api":      RegisterClusterAPI,
		"producer": RegisterClusterProducer,
		"worker":   RegisterClusterWorker,
		"ingester": RegisterClusterIngester,
	}
	for name, register := range registers {
		t.Run(name, func(t *testing.T) {
			container := dig.New()
			require.NoError(t, register(container, clusterConfig(t)))

			require.NoError(t, container.Invoke(func(bus events.EventBus, client *asynq.Client, inspector *asynq.Inspector) {
				closeEventBus(t, bus)
				t.Cleanup(func() { assert.NoError(t, client.Close()) })
				t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
				ctx := context.Background()
				queued := func() int {
					info, err := inspector.GetQueueInfo("notification")
					if err != nil {
						return 0 // the queue exists once a task is queued
					}
					return info.Pending
				}

				// Other topics go over pub/sub, not to the queue
				require.NoError(t, events.Publish(ctx, bus, events.HeartbeatEvent, shared.HeartBeatModel{MonitorID: "mon-1"}))
				require.NoError(t, events.Publish(ctx, bus, events.MonitorStatusChanged, shared.HeartBeatModel{MonitorID: "mon-1"}))
				assert.Equal(t, 0, queued(), "tasks queued by other topics")

				require.NoError(t, events.Publish(ctx, bus, events.ImportantHeartbeat, shared.HeartBeatModel{MonitorID: "mon-1"}))
				assert.Equal(t, 1, queued(), "tasks queued after an important heartbeat")
				require.NoError(t, events.Publish(ctx, bus, certificate.CertificateExpiry, certificate.CertificateExpiryEvent{MonitorID: "mon-1"}))
				assert.Equal(t, 2, queued(), "tasks queued after a certificate expiry")
				require.NoError(t, events.Publish(ctx, bus, stale.MonitorStale, stale.Event{MonitorID: "mon-1"}))
				assert.Equal(t, 3, queued(), "tasks queued after a stale monitor")
				require.NoError(t, events.Publish(ctx, bus, stale.MonitorStaleRecovered, stale.Event{MonitorID: "mon-1"}))
				assert.Equal(t, 4, queued(), "tasks queued after a recovered stale monitor")
			}))
		})
	}
}

// A provide that dig refuses used to be dropped, and the first provider of the type won
func TestRegisterClusterAPI_FailsNamingTheConstructorDigRefuses(t *testing.T) {
	container := dig.New()
	require.NoError(t, container.Provide(func() *zap.SugaredLogger { return zap.NewNop().Sugar() }))

	err := RegisterClusterAPI(container, clusterConfig(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to provide peekaping/internal.ProvideLogger:")
}

func TestRegister_RefusesAnInvalidTrustedProxy(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.TrustedProxies = "10.0.0.0/8,my-proxy"
	container := dig.New()
	require.NoError(t, Register(container, cfg))

	err := container.Invoke(func(*httpx.TrustedProxies) {})

	assert.ErrorContains(t, err, `invalid TRUSTED_PROXIES entry "my-proxy"`)
}

func TestRegister_ProvidesTheTrustedProxiesFromTheConfig(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.TrustedProxies = "10.0.0.0/8"
	container := dig.New()
	require.NoError(t, Register(container, cfg))

	require.NoError(t, container.Invoke(func(proxies *httpx.TrustedProxies) {
		for remote, want := range map[string]string{"10.0.0.1:1234": "198.51.100.7", "192.0.2.1:1234": "192.0.2.1"} {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = remote
			r.Header.Set("X-Forwarded-For", "198.51.100.7")
			assert.Equal(t, want, proxies.ClientIP(r), remote)
		}
	}))
}

func TestRegister_RefusesAnInvalidCORSOrigin(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.CORSOrigins = "https://peek.example,example.com"
	container := dig.New()
	require.NoError(t, Register(container, cfg))

	err := container.Invoke(func(*httpx.CORSOrigins) {})

	assert.ErrorContains(t, err, `invalid CORS_ORIGINS entry "example.com"`)
}

func TestRegister_ProvidesTheCORSOriginsFromTheConfig(t *testing.T) {
	for name, tc := range map[string]struct{ origins, clientURL, allowed, denied string }{
		"the list": {"https://a.example,https://b.example", "https://peek.example", "https://b.example", "https://peek.example"},
		"the origin of CLIENT_URL when the list is unset": {"", "https://peek.example/status", "https://peek.example", "https://a.example"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := clusterConfig(t)
			cfg.CORSOrigins = tc.origins
			cfg.ClientURL = tc.clientURL
			container := dig.New()
			require.NoError(t, Register(container, cfg))

			require.NoError(t, container.Invoke(func(origins *httpx.CORSOrigins) {
				assert.Equal(t, tc.allowed, origins.AllowOrigin(tc.allowed))
				assert.Empty(t, origins.AllowOrigin(tc.denied))
			}))
		})
	}
}

func TestRegister_RefusesAnUnsupportedDBType(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.DBType = "oracle"

	err := Register(dig.New(), cfg)

	assert.EqualError(t, err, `unsupported DB_TYPE "oracle"`)
}
