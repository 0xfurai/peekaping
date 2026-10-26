//go:build soak

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/tag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestSoak runs 1,000 monitors checked every 20 seconds through the scheduler, with a pool of
// 128 checks, the real ingest path over SQLite and the watchdog on an in-memory store, for
// SOAK_DURATION (5m by default). The checks are fakes that answer in 10 to 500 ms, Down one
// time in ten. From apps/server:
//
//	SOAK_DURATION=3m go test -tags soak -run TestSoak -count=1 -timeout 10m -v ./internal/modules/scheduler/
//	SOAK_DURATION=1h go test -tags soak -run TestSoak -count=1 -timeout 75m -v ./internal/modules/scheduler/
//
// It fails if a monitor goes longer than its check interval plus soakSlack without a
// heartbeat, has fewer heartbeats than its due checks less one, the watchdog finds a stale
// monitor, or the heap in use or the goroutines grow past their bounds after the first minute.
func TestSoak(t *testing.T) {
	const (
		monitors    = 1000
		interval    = 20 * time.Second
		concurrency = 128
		// soakSlack is how much longer than the check interval two heartbeats of a monitor may
		// be apart: half an interval. A missed check leaves about two intervals between them.
		soakSlack = interval / 2
		// After the first minute, the heap in use may grow by half and the goroutines by 20
		heapGrowth      = 1.5
		goroutineGrowth = 20
	)
	duration := 5 * time.Minute
	if v := os.Getenv("SOAK_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		require.NoError(t, err, "SOAK_DURATION")
		duration = d
	}
	ctx := context.Background()
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stderr),
		zap.WarnLevel,
	)).Sugar()
	container := newSQLiteContainer(t, &config.Config{CheckConcurrency: concurrency}, logger)

	var (
		s        *Scheduler
		watchdog *stale.Watchdog
		store    stale.Store
		db       *bun.DB
	)
	require.NoError(t, container.Invoke(func(
		loader *check.Loader,
		monitorService monitor.Service,
		supervisor *supervisor.HealthCheckSupervisor,
		ingesterService ingester.Service,
		heartbeatService heartbeat.Service,
		staleStore stale.Store,
		bus events.EventBus,
		sqlDB *bun.DB,
	) error {
		for i := range monitors {
			if _, err := monitorService.Create(ctx, &monitor.CreateUpdateDto{
				Type:          "http",
				Name:          fmt.Sprintf("Soak %04d", i),
				Interval:      int(interval / time.Second),
				Timeout:       16,
				MaxRetries:    i % 3,
				RetryInterval: int(interval / time.Second),
				Active:        true,
				Config:        `{"url":"https://example.com"}`,
			}); err != nil {
				return err
			}
		}
		s = New(Config{
			Source:      NewDatabaseSource(monitorService, loader, bus),
			Executors:   executorsByType{"http": soakExecutor{}},
			Supervisor:  supervisor,
			Ingester:    ingesterService,
			Concurrency: concurrency,
			Logger:      logger,
		})
		watchdog = stale.NewWatchdog(stale.WatchdogConfig{
			Store:        staleStore,
			Rechecker:    s,
			Monitors:     monitorService,
			Heartbeats:   heartbeatService,
			EventBus:     bus,
			CheckTimeout: CheckTimeout,
			Logger:       logger,
		})
		store, db = staleStore, sqlDB
		return nil
	}))

	// Start halfway between two rounds of checks, so that the samples, a whole number of
	// minutes later, are taken while no checks are running
	time.Sleep(time.Until(nextAligned(time.Now().Add(-interval/2), interval).Add(interval / 2)))
	startedAt := time.Now()
	require.NoError(t, s.Start(ctx))
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	watchdogDone := make(chan struct{})
	go func() {
		watchdog.Run(watchdogCtx)
		close(watchdogDone)
	}()
	t.Logf("%d monitors every %s, pool of %d, for %s", monitors, interval, concurrency, duration)

	type sample struct {
		heapInUse  uint64
		goroutines int
		stale      int64
	}
	var samples []sample
	endAt := startedAt.Add(duration)
	for at := startedAt.Add(time.Minute); !at.After(endAt); at = at.Add(time.Minute) {
		time.Sleep(time.Until(at))
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		staleCount, err := store.Count(ctx)
		require.NoError(t, err)
		smp := sample{heapInUse: mem.HeapInuse, goroutines: runtime.NumGoroutine(), stale: staleCount}
		samples = append(samples, smp)
		t.Logf("%5s  heap in use %6.1f MiB  goroutines %4d  stale %d",
			time.Since(startedAt).Round(time.Second), float64(smp.heapInUse)/(1<<20), smp.goroutines, smp.stale)
	}
	time.Sleep(time.Until(endAt))
	stoppedAt := time.Now()
	stopWatchdog()
	<-watchdogDone
	s.Stop()

	// The heartbeats, by monitor, in time order
	var rows []struct {
		MonitorID string    `bun:"monitor_id"`
		Time      time.Time `bun:"time"`
	}
	require.NoError(t, db.NewSelect().Table("heartbeats").Column("monitor_id", "time").Scan(ctx, &rows))
	beats := make(map[string][]time.Time)
	for _, row := range rows {
		beats[row.MonitorID] = append(beats[row.MonitorID], row.Time)
	}
	require.Len(t, beats, monitors, "every monitor has heartbeats")

	// A check right away, then one on each multiple of the interval until the stop
	due := 1
	for b := nextAligned(startedAt, interval); !b.After(stoppedAt); b = b.Add(interval) {
		due++
	}
	var (
		fewest   = due
		worstGap time.Duration
		tooLong  int // gaps longer than interval + soakSlack
		lateness []time.Duration
	)
	for id, times := range beats {
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		fewest = min(fewest, len(times))
		if first := times[0].Sub(startedAt); first > soakSlack {
			tooLong++
			t.Errorf("monitor %s: first heartbeat %s after the start", id, first)
		}
		edges := append(slices.Clone(times), stoppedAt)
		for i := 1; i < len(edges); i++ {
			gap := edges[i].Sub(edges[i-1])
			worstGap = max(worstGap, gap)
			if gap > interval+soakSlack {
				tooLong++
			}
		}
		for _, at := range times[1:] {
			lateness = append(lateness, at.Sub(at.Truncate(interval)))
		}
	}
	slices.Sort(lateness)
	percentile := func(p float64) time.Duration { return lateness[int(p*float64(len(lateness)-1))] }
	t.Logf("heartbeats %d (due %d per monitor, fewest %d); worst gap %s; heartbeat after its due time p50 %s p99 %s max %s",
		len(rows), due, fewest, worstGap.Round(time.Millisecond),
		percentile(0.5).Round(time.Millisecond), percentile(0.99).Round(time.Millisecond), lateness[len(lateness)-1].Round(time.Millisecond))

	assert.Zero(t, tooLong, "gaps between heartbeats longer than the check interval + %s (worst %s)", soakSlack, worstGap)
	assert.GreaterOrEqual(t, fewest, due-1, "every monitor has a heartbeat for each due check, less one")

	staleCount, err := store.Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, staleCount, "the watchdog finds no stale monitor")
	if duration >= stale.WatchdogInterval {
		ranAt, err := store.RanAt(ctx)
		require.NoError(t, err)
		assert.NotNil(t, ranAt, "the watchdog ran")
	}
	for i, smp := range samples {
		assert.Zero(t, smp.stale, "no stale monitor at minute %d", i+1)
	}

	if len(samples) < 2 {
		t.Logf("too short to compare memory after the first minute")
		return
	}
	warm := samples[0]
	for i, smp := range samples[1:] {
		assert.LessOrEqual(t, float64(smp.heapInUse), heapGrowth*float64(warm.heapInUse),
			"heap in use at minute %d, against %.1f MiB at minute 1", i+2, float64(warm.heapInUse)/(1<<20))
		assert.LessOrEqual(t, smp.goroutines, warm.goroutines+goroutineGrowth,
			"goroutines at minute %d, against %d at minute 1", i+2, warm.goroutines)
	}
}

// soakExecutor answers a check after 10 to 500 ms, Down one time in ten
type soakExecutor struct{}

func (soakExecutor) Execute(ctx context.Context, _ *executor.Monitor, _ *executor.Proxy) *executor.Result {
	start := time.Now().UTC()
	timer := time.NewTimer(10*time.Millisecond + rand.N(490*time.Millisecond))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return executor.DownResult(ctx.Err(), start, time.Now().UTC())
	}
	if rand.IntN(10) == 0 {
		return &executor.Result{Status: shared.MonitorStatusDown, Message: "503 - Service Unavailable", StartTime: start, EndTime: time.Now().UTC()}
	}
	return &executor.Result{Status: shared.MonitorStatusUp, Message: "200 - OK", StartTime: start, EndTime: time.Now().UTC()}
}

func (soakExecutor) Validate(string) error         { return nil }
func (soakExecutor) Unmarshal(string) (any, error) { return nil, nil }

// newSQLiteContainer registers the real services the scheduler's source, supervisor and ingest
// need over a migrated SQLite database, as a single-mode binary would, with the given config: the
// event bus and the stale monitors are in memory. The test builds the scheduler and the watchdog
// itself.
func newSQLiteContainer(t testing.TB, cfg *config.Config, logger *zap.SugaredLogger) *dig.Container {
	t.Helper()
	cfg.DBType = "sqlite"
	cfg.DBName = filepath.Join(t.TempDir(), "scheduler.db")

	container := dig.New()
	provide := func(constructor any) {
		t.Helper()
		require.NoError(t, container.Provide(constructor))
	}
	provide(func() *config.Config { return cfg })
	provide(func() *zap.SugaredLogger { return logger })
	provide(infra.ProvideSQLDB)
	provide(func() events.EventBus { return newBus(t) })
	provide(stale.ProvideMemoryStore)

	require.NoError(t, errors.Join(
		heartbeat.RegisterDependencies(container),
		healthcheck.RegisterDependencies(container),
		tag.RegisterDependencies(container),
		monitor_tag.RegisterDependencies(container),
		monitor.RegisterDependencies(container),
		proxy.RegisterDependencies(container),
		maintenance.RegisterDependencies(container),
		monitor_maintenance.RegisterDependencies(container),
		monitor_notification.RegisterDependencies(container),
		setting.RegisterDependencies(container),
		notification_sent_history.RegisterDependencies(container),
		monitor_tls_info.RegisterDependencies(container),
		certificate.RegisterDependencies(container),
		stats.RegisterDependencies(container),
		check.RegisterDependencies(container),
		ingester.RegisterDependencies(container),
	))

	require.NoError(t, container.Invoke(func(db *bun.DB) error {
		t.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		if err := migrator.Init(ctx); err != nil {
			return err
		}
		_, err := migrator.Migrate(ctx)
		return err
	}))
	return container
}
