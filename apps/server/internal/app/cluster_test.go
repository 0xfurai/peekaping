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
	"peekaping/internal/dbtx"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/worker"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

	// What cmd/api invokes: the cleanup cron with its run lock, the settings, the listeners, the
	// status repair, the metrics collector, the server, and the database it closes on shutdown
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

// Every api replica runs the hourly cleanup's cron, so the replicas share its run lock in Redis
func TestRegisterClusterAPI_ProvidesTheRunLockInRedis(t *testing.T) {
	cfg := clusterConfig(t)
	hour := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	claim := func(replica string) bool {
		container := dig.New()
		require.NoError(t, RegisterClusterAPI(container, cfg), replica)
		var claimed bool
		require.NoError(t, container.Invoke(func(lock cleanup.RunLock, rdb *redis.Client) {
			t.Cleanup(func() { assert.NoError(t, rdb.Close()) })
			var err error
			claimed, err = lock.Claim(context.Background(), hour)
			require.NoError(t, err)
		}), replica)
		return claimed
	}

	assert.True(t, claim("first replica"), "first replica's claim of the hour")
	assert.False(t, claim("second replica"), "second replica's claim of the same hour")
}

func TestRegisterClusterProducer_ResolvesWhatCmdProducerRuns(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterProducer(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		_ *producer.Producer,
		_ *producer.EventListener,
		_ dbtx.Transactor,
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

// The sweep runs on the leader producer, beside the watchdog, and sends its events over the
// cluster's bus. The api serves requests, and a replica of it must not run a sweep of its own.
func TestRegisterClusterProducer_ResolvesTheProbeSweepAndTheAPIDoesNot(t *testing.T) {
	producerContainer := dig.New()
	require.NoError(t, RegisterClusterProducer(producerContainer, clusterConfig(t)))
	require.NoError(t, producerContainer.Invoke(func(
		sweep *probe.Sweep,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
		assert.NotNil(t, sweep)
	}))

	apiContainer := dig.New()
	require.NoError(t, RegisterClusterAPI(apiContainer, clusterConfig(t)))
	assert.Error(t, apiContainer.Invoke(func(*probe.Sweep) {}), "only the producer runs the sweep")
}

func TestRegisterClusterProducer_TheWatchdogSkipsOnlyTheMonitorsOfAnOfflineRemoteProbe(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterProducer(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		probeOffline stale.ProbeOfflineFunc,
		monitors monitor.Service,
		probes probe.Service,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
		require.NoError(t, migrateDB(db))

		assertWatchdogSkipsOnlyOfflineRemote(t, probeOffline, monitors, probes)
	}))
}

// assertWatchdogSkipsOnlyOfflineRemote checks the watchdog's question against the real services: a
// monitor of the local probe is never skipped, and one of a remote probe is while it is offline
// (a probe starts offline) and not once it is online.
func assertWatchdogSkipsOnlyOfflineRemote(t *testing.T, probeOffline stale.ProbeOfflineFunc, monitors monitor.Service, probes probe.Service) {
	t.Helper()
	ctx := context.Background()
	remote, err := probes.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)
	onLocal, err := monitors.Create(ctx, &monitor.CreateUpdateDto{Type: "http", Name: "Home", Active: true, Config: "{}"})
	require.NoError(t, err)
	onRemote, err := monitors.Create(ctx, &monitor.CreateUpdateDto{Type: "http", Name: "Checkout", Active: true, Config: "{}", ProbeIDs: []string{remote.ID}})
	require.NoError(t, err)

	offline, err := probeOffline(ctx, onLocal.ID)
	require.NoError(t, err)
	assert.False(t, offline, "the local probe is never offline")
	offline, err = probeOffline(ctx, onRemote.ID)
	require.NoError(t, err)
	assert.True(t, offline, "a probe that never connected is offline")

	changed, err := probes.MarkOnline(ctx, remote.ID)
	require.NoError(t, err)
	require.True(t, changed)
	offline, err = probeOffline(ctx, onRemote.ID)
	require.NoError(t, err)
	assert.False(t, offline, "and not once it is online")
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

// The usage ping reports cluster mode whatever MODE says, and the api replicas race for the day's
// ping in Redis.
func TestRegisterClusterAPI_RunsTheUsagePingInClusterModeClaimedInRedis(t *testing.T) {
	container := dig.New()
	cfg := clusterConfig(t)
	cfg.Mode = "dev"
	require.NoError(t, RegisterClusterAPI(container, cfg))

	require.NoError(t, container.Invoke(func(
		mode telemetry.Mode,
		claimer telemetry.Claimer,
		_ *telemetry.Sender,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
		assert.Equal(t, telemetry.ModeCluster, mode)
		assert.IsType(t, &telemetry.RedisClaimer{}, claimer)
	}))
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

// warningsAtStartup registers a binary's graph in a container whose logger writes to an observer,
// and returns the warnings logged on the way. The container's logger is decorated, since dig
// refuses a second provider of *zap.SugaredLogger. The probe line proves the observer is the
// logger the graph uses, so a binary that logs nothing can't pass for lack of one.
func warningsAtStartup(t *testing.T, register func(*dig.Container, *config.Config) error, cfg *config.Config) []observer.LoggedEntry {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	container := dig.New()
	require.NoError(t, container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger {
		return zap.New(core).Sugar()
	}))
	require.NoError(t, register(container, cfg))
	require.NoError(t, container.Invoke(func(logger *zap.SugaredLogger) { logger.Info("probe") }))
	require.Len(t, logs.FilterMessage("probe").All(), 1, "the graph's logger isn't the observer")
	return logs.FilterLevelExact(zapcore.WarnLevel).All()
}

// In cluster mode the api, producer and ingester open the same SQLite file, which SQLite lets one
// process write to at a time. It works, but single mode or PostgreSQL fits better, and nothing
// else tells the operator so. The worker has no database, PostgreSQL takes concurrent writers
// and single mode is one process, so none of them warns.
func TestRegisterCluster_WarnsAboutSQLite(t *testing.T) {
	registers := map[string]func(*dig.Container, *config.Config) error{
		"api":      RegisterClusterAPI,
		"producer": RegisterClusterProducer,
		"ingester": RegisterClusterIngester,
	}
	for name, register := range registers {
		t.Run(name+" on SQLite warns once", func(t *testing.T) {
			warnings := warningsAtStartup(t, register, clusterConfig(t))

			require.Len(t, warnings, 1)
			message := warnings[0].Message
			assert.NotContains(t, message, "\n", "one log line")
			assert.Contains(t, message, "share one database file and write to it one at a time")
			assert.Contains(t, message, "Single mode (the SQLite bundle image) or PostgreSQL fits better")
			assert.Contains(t, message, "https://docs.peekaping.com/self-hosting/docker-with-sqlite#cluster-mode")
		})
		t.Run(name+" on PostgreSQL doesn't", func(t *testing.T) {
			cfg := clusterConfig(t)
			cfg.DBType, cfg.DBName = "postgres", "peekaping"

			assert.Empty(t, warningsAtStartup(t, register, cfg))
		})
	}

	t.Run("worker doesn't, whatever DB_TYPE says", func(t *testing.T) {
		assert.Empty(t, warningsAtStartup(t, RegisterClusterWorker, clusterConfig(t)))
	})
	t.Run("single mode on SQLite doesn't", func(t *testing.T) {
		assert.Empty(t, warningsAtStartup(t, RegisterSingle, singleConfig(t)))
	})
}
