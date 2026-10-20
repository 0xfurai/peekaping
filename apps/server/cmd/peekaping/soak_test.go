//go:build soak

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestSoak runs 1,000 HTTP monitors checked every 20 seconds through the whole single-mode app,
// as peekaping runs it: created through the API, checked by the scheduler's pool of 128, stored
// over SQLite with the stats, the websocket and the listeners on the in-memory event bus, and
// watched by the watchdog. It runs for SOAK_DURATION (5m by default) after the monitors are
// created. The executors can't be swapped through the app, so the checks are real HTTP checks
// of a local target that answers in 10 to 500 ms, 503 one time in ten. From apps/server:
//
//	SOAK_DURATION=3m go test -tags soak -run TestSoak -count=1 -timeout 15m -v ./cmd/peekaping/
//	SOAK_DURATION=24h go test -tags soak -run TestSoak -count=1 -timeout 25h -v ./cmd/peekaping/
//
// It fails if a monitor goes longer than its check interval plus soakSlack without a
// heartbeat, has fewer heartbeats than its due checks less one, the health endpoint reports a
// stale monitor, or the heap in use or the goroutines grow past their bounds after the first
// minute.
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

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := time.NewTimer(10*time.Millisecond + rand.N(490*time.Millisecond))
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
		if rand.IntN(10) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(target.Close)

	cfg := loadConfig(t, map[string]string{
		"CHECK_CONCURRENCY": strconv.Itoa(concurrency),
		"LOG_LEVEL":         "warn",
	})
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stderr),
		zap.WarnLevel,
	)).Sugar()
	in := startPeekaping(t, cfg, logger, nil)
	in.registerAdmin(t)

	createdAt := time.Now()
	for i := range monitors {
		mon := httpMonitor(fmt.Sprintf("Soak %04d", i), target.URL, int(interval/time.Second))
		mon["max_retries"] = i % 3
		in.createMonitor(t, mon)
	}
	t.Logf("created %d monitors through the API in %s", monitors, time.Since(createdAt).Round(time.Millisecond))

	// Start measuring halfway between two rounds of checks, so that the samples, a whole number
	// of minutes later, are taken while no checks are running
	time.Sleep(time.Until(nextRound(time.Now().Add(interval), interval).Add(interval / 2)))
	startedAt := time.Now()
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
		smp := sample{heapInUse: mem.HeapInuse, goroutines: runtime.NumGoroutine(), stale: in.staleMonitors(t)}
		samples = append(samples, smp)
		t.Logf("%5s  heap in use %6.1f MiB  goroutines %4d  stale %d",
			time.Since(startedAt).Round(time.Second), float64(smp.heapInUse)/(1<<20), smp.goroutines, smp.stale)
	}
	time.Sleep(time.Until(endAt))
	stoppedAt := time.Now()
	watchdogRan := in.watchdogRanAt(t)
	staleAtEnd := in.staleMonitors(t)
	require.NoError(t, in.stop(t))

	// The heartbeats, by monitor, in time order, read once peekaping has closed the database
	db, err := infra.ProvideSQLDB(&config.Config{DBType: cfg.DBType, DBName: cfg.DBName}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
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

	// One check on each multiple of the interval between the start and the stop
	due := 0
	for b := nextRound(startedAt, interval); !b.After(stoppedAt); b = b.Add(interval) {
		due++
	}
	var (
		fewest   = due
		measured int
		worstGap time.Duration
		tooLong  int // gaps longer than interval + soakSlack
		lateness []time.Duration
	)
	for id, times := range beats {
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		// The heartbeats from the last one before the start up to the stop
		from := slices.IndexFunc(times, func(at time.Time) bool { return at.After(startedAt) })
		if from < 0 {
			from = len(times)
		}
		window := slices.DeleteFunc(slices.Clone(times[max(from-1, 0):]), func(at time.Time) bool { return at.After(stoppedAt) })
		inWindow := 0
		for _, at := range window {
			if at.After(startedAt) {
				inWindow++
				lateness = append(lateness, at.Sub(nextRound(at, interval).Add(-interval)))
			}
		}
		fewest = min(fewest, inWindow)
		measured += inWindow

		edges := append(window, stoppedAt)
		for i := 1; i < len(edges); i++ {
			gap := edges[i].Sub(edges[i-1])
			worstGap = max(worstGap, gap)
			if gap > interval+soakSlack {
				tooLong++
				if tooLong <= 10 {
					t.Errorf("monitor %s: no heartbeat between %s and %s", id, edges[i-1].Format(time.TimeOnly), edges[i].Format(time.TimeOnly))
				}
			}
		}
	}
	require.NotEmpty(t, lateness, "heartbeats between the start and the stop")
	slices.Sort(lateness)
	percentile := func(p float64) time.Duration { return lateness[int(p*float64(len(lateness)-1))] }
	t.Logf("heartbeats %d in the window (due %d per monitor, fewest %d); worst gap %s; heartbeat after its due time p50 %s p99 %s max %s",
		measured, due, fewest, worstGap.Round(time.Millisecond),
		percentile(0.5).Round(time.Millisecond), percentile(0.99).Round(time.Millisecond), lateness[len(lateness)-1].Round(time.Millisecond))

	assert.Zero(t, tooLong, "gaps between heartbeats longer than the check interval + %s (worst %s)", soakSlack, worstGap)
	assert.GreaterOrEqual(t, fewest, due-1, "every monitor has a heartbeat for each due check, less one")

	assert.Zero(t, staleAtEnd, "the watchdog finds no stale monitor")
	if duration >= time.Minute {
		assert.NotNil(t, watchdogRan, "the watchdog ran")
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

// nextRound is the next multiple of the check interval after the time, counted from the Unix
// epoch: when the scheduler checks the monitors next
func nextRound(after time.Time, interval time.Duration) time.Time {
	ms, p := after.UnixMilli(), interval.Milliseconds()
	return time.UnixMilli((ms/p + 1) * p)
}

// health is what the health endpoint reports
type health struct {
	StaleMonitors *int64     `json:"stale_monitors"`
	WatchdogRanAt *time.Time `json:"watchdog_ran_at"`
}

func (in *instance) health(t testing.TB) health {
	t.Helper()
	status, body := in.request(t, http.MethodGet, "/api/v1/health", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var h health
	require.NoError(t, json.Unmarshal(body, &h), string(body))
	return h
}

// staleMonitors is how many stale monitors the health endpoint reports
func (in *instance) staleMonitors(t testing.TB) int64 {
	t.Helper()
	h := in.health(t)
	require.NotNil(t, h.StaleMonitors, "the health endpoint reads the stale monitors")
	return *h.StaleMonitors
}

// watchdogRanAt is when the health endpoint says the watchdog last ran
func (in *instance) watchdogRanAt(t testing.TB) *time.Time {
	t.Helper()
	return in.health(t).WatchdogRanAt
}
