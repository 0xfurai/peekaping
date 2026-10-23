package app

import (
	"context"
	"errors"
	"net"
	"runtime"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	"peekaping/internal/reporting/reportingtest"
	"peekaping/internal/version"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

const testDSN = "https://public@sentry.invalid/1"

func withTransport(recorder *reportingtest.Recorder) func(*reporting.Options) {
	return func(o *reporting.Options) { o.Transport = recorder }
}

// What a report of each binary says about its install: the version, the mode its registration
// gives it (not the MODE setting), the storage backend, and the OS and architecture
func TestSetupErrorReporting_ReportsTheReleaseTheModeTheStorageAndThePlatform(t *testing.T) {
	type binary struct {
		register func(*dig.Container, *config.Config) error
		config   func(*testing.T) *config.Config
		mode, db string
	}
	noDatabase := func(t *testing.T) *config.Config {
		cfg := clusterConfig(t)
		cfg.DBType, cfg.DBName = "", ""
		return cfg
	}
	postgres := func(t *testing.T) *config.Config {
		cfg := clusterConfig(t)
		cfg.DBType = "postgres"
		return cfg
	}
	for name, tc := range map[string]binary{
		"peekaping":         {RegisterSingle, singleConfig, "single", "sqlite"},
		"api":               {RegisterClusterAPI, clusterConfig, "cluster", "sqlite"},
		"producer":          {RegisterClusterProducer, clusterConfig, "cluster", "sqlite"},
		"ingester":          {RegisterClusterIngester, clusterConfig, "cluster", "sqlite"},
		"worker":            {RegisterClusterWorker, noDatabase, "cluster", "unknown"},
		"api on PostgreSQL": {RegisterClusterAPI, postgres, "cluster", "postgres"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &reportingtest.Recorder{}
			cfg := tc.config(t)
			cfg.Mode = "prod" // MODE is dev, prod or test, and says nothing about single or cluster
			cfg.ErrorReportingDSN = testDSN
			container := dig.New()
			require.NoError(t, tc.register(container, cfg)) // provides the database, and connects to it only if something asks
			t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })

			require.NoError(t, SetupErrorReporting(container, cfg, withTransport(recorder)))
			reporting.SetEnabled(true)
			reporting.CapturePanic("test", "boom", nil)

			require.Len(t, recorder.Events(), 1)
			event := recorder.Events()[0]
			assert.Equal(t, version.Version, event.Release)
			assert.Equal(t, map[string]string{"mode": tc.mode, "db": tc.db, "os": runtime.GOOS + "/" + runtime.GOARCH, "component": "test"}, event.Tags)
		})
	}
}

func TestSetupErrorReporting_SendsNothingWhenTheEnvironmentForcesItOff(t *testing.T) {
	for name, set := range map[string]func(*config.Config){
		"ERROR_REPORTING=off": func(c *config.Config) { c.ErrorReporting = "off" },
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &reportingtest.Recorder{}
			cfg := singleConfig(t)
			cfg.ErrorReportingDSN = testDSN
			set(cfg)
			container := dig.New()
			require.NoError(t, RegisterSingle(container, cfg))
			t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })

			require.NoError(t, SetupErrorReporting(container, cfg, withTransport(recorder)))
			reporting.SetEnabled(true) // the admin's choice
			reporting.CapturePanic("test", "boom", nil)

			assert.False(t, reporting.Enabled())
			assert.Empty(t, recorder.Events())
		})
	}
}

func clusterConfigWithRedis(t *testing.T) (*config.Config, *miniredis.Miniredis) {
	t.Helper()
	redis := miniredis.RunT(t)
	cfg := clusterConfig(t)
	host, port, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	cfg.RedisHost, cfg.RedisPort = host, port
	return cfg, redis
}

// AC6: a change in Settings reaches the cluster's processes without a restart, even the worker,
// which has no database
func TestStartErrorReportingRefresher_AWorkerWithoutADatabaseFollowsTheKeyInRedis(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	cfg, redis := clusterConfigWithRedis(t)
	cfg.DBType, cfg.DBName = "", ""
	container := dig.New()
	require.NoError(t, RegisterClusterWorker(container, cfg))
	require.NoError(t, container.Invoke(func(bus events.EventBus, client *asynq.Client, inspector *asynq.Inspector) {
		closeEventBus(t, bus)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done, err := StartErrorReportingRefresher(ctx, container, 10*time.Millisecond)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	assert.False(t, reporting.Enabled(), "a missing key leaves reporting off")

	// The api turns it on in Settings, and the worker follows within a refresh
	require.NoError(t, redis.Set(redissync.Key, "true"))
	require.Eventually(t, reporting.Enabled, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, redis.Set(redissync.Key, "false"))
	require.Eventually(t, func() bool { return !reporting.Enabled() }, 2*time.Second, 5*time.Millisecond)

	// A key that goes missing, as when Redis was flushed, leaves it as it is
	require.NoError(t, redis.Set(redissync.Key, "true"))
	require.Eventually(t, reporting.Enabled, 2*time.Second, 5*time.Millisecond)
	redis.Del(redissync.Key)
	time.Sleep(100 * time.Millisecond)
	assert.True(t, reporting.Enabled())

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the refresher did not stop with its context")
	}
}

func TestStartErrorReportingRefresher_FailsWithoutRedis(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	_, err := StartErrorReportingRefresher(context.Background(), container, time.Minute)

	assert.Error(t, err, "single mode has no Redis, so nothing to refresh from")
}

type fakeErrorReportingService struct {
	error_reporting.Service
	applied int
	err     error
}

func (f *fakeErrorReportingService) Apply(context.Context) error {
	f.applied++
	return f.err
}

func TestApplyErrorReporting_AsksTheServiceToApplyTheStoredChoice(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))
	service := &fakeErrorReportingService{}
	require.NoError(t, container.Decorate(func(error_reporting.Service) error_reporting.Service { return service }))

	require.NoError(t, ApplyErrorReporting(context.Background(), container))
	assert.Equal(t, 1, service.applied)

	service.err = errors.New("no such table: settings")
	assert.ErrorContains(t, ApplyErrorReporting(context.Background(), container), "no such table")
}

// runRecordingService is a service whose Run says how often it was asked to apply, and stops with
// its context
type runRecordingService struct {
	error_reporting.Service
	every chan time.Duration
}

func (r *runRecordingService) Run(ctx context.Context, every time.Duration) {
	r.every <- every
	<-ctx.Done()
}

// The cluster api republishes the stored choice to Redis every interval, so that a publish that
// missed a Redis that was down is made up
func TestStartErrorReportingPublisher_RunsTheServiceUntilItsContextIsDone(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))
	service := &runRecordingService{every: make(chan time.Duration, 1)}
	require.NoError(t, container.Decorate(func(error_reporting.Service) error_reporting.Service { return service }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done, err := StartErrorReportingPublisher(ctx, container, redissync.DefaultEvery)

	require.NoError(t, err)
	select {
	case every := <-service.every:
		assert.Equal(t, time.Minute, every)
	case <-time.After(2 * time.Second):
		t.Fatal("the service was not asked to run")
	}
	select {
	case <-done:
		t.Fatal("the publisher stopped before its context was done")
	default:
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the publisher did not stop with its context")
	}
}
