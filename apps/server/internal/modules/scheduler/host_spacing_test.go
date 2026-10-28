package scheduler

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// spacingClock is the clock of a host spacer in a test: it stands still until the test moves it, and
// the waits of the checks are recorded, not slept
type spacingClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (c *spacingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *spacingClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *spacingClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	return nil
}

func (c *spacingClock) slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.waits)
}

// withHostSpacing makes the scheduler's supervisor, over the real registry (which knows the host of
// a tcp monitor), space the checks to one host at perMinute
func withHostSpacing(perMinute int, clock *spacingClock) (func(*Config), *supervisor.HostSpacer) {
	spacer := supervisor.NewHostSpacerWithClock(perMinute, clock.Now, clock.Sleep)
	return func(cfg *Config) {
		checks := supervisor.NewHealthCheck(nil, executor.NewExecutorRegistry(zap.NewNop().Sugar()), zap.NewNop().Sugar())
		checks.SetHostSpacer(spacer)
		cfg.Supervisor = checks
	}, spacer
}

func tcpMonitorAt(id, host string, seconds int) []monitorOption {
	return []monitorOption{ofType("tcp"), withInterval(seconds), func(m *shared.Monitor) {
		m.Config = `{"host":"` + host + `","port":80}`
	}}
}

func TestScheduler_SpacesTheChecksOfTenMonitorsOnOneHost(t *testing.T) {
	// Not parallel: building the registry registers validators on a shared instance
	clock := &spacingClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	option, _ := withHostSpacing(120, clock)
	h := newHarness(t, option)
	for i := range 10 {
		id := "mon-" + strconv.Itoa(i)
		h.db.put(monitorWith(id, tcpMonitorAt(id, "shared.example.com", 60)...))
	}

	h.start()

	eventually(t, 5*time.Second, func() bool { return h.exec.count() == 10 }, "all ten run: the farthest turn, 4.5s, is within the 5s a 60s interval may wait")
	// The one that has its turn at once never sleeps, the other nine each sleep until theirs
	offsets := append([]time.Duration{0}, clock.slept()...)
	slices.Sort(offsets)
	require.Len(t, offsets, 10)
	for i := 1; i < len(offsets); i++ {
		assert.GreaterOrEqual(t, offsets[i]-offsets[i-1], 500*time.Millisecond, "120 a minute: no two checks within half a second, in %v", offsets)
	}
}

func TestScheduler_WithoutACapTheChecksToOneHostAllRunAtOnce(t *testing.T) {
	// Not parallel: building the registry registers validators on a shared instance
	clock := &spacingClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	option, _ := withHostSpacing(0, clock)
	h := newHarness(t, option)
	for i := range 10 {
		id := "mon-" + strconv.Itoa(i)
		h.db.put(monitorWith(id, tcpMonitorAt(id, "shared.example.com", 60)...))
	}

	h.start()

	eventually(t, 5*time.Second, func() bool { return h.exec.count() == 10 }, "all ten run")
	assert.Empty(t, clock.slept(), "none waited")
}

func TestScheduler_ASkippedTickLeavesNoHeartbeatAndTheNextOneRuns(t *testing.T) {
	// Not parallel: building the registry registers validators on a shared instance
	clock := &spacingClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	option, spacer := withHostSpacing(60, clock)
	h := newHarness(t, option)
	// Seven turns taken: the next is 7s away, more than the 5s a 20s interval may wait
	for range 7 {
		spacer.Reserve([]string{"busy.example.com"}, time.Hour)
	}
	h.db.put(monitorWith("a", tcpMonitorAt("a", "busy.example.com", 20)...))
	h.start()

	require.Eventually(t, func() bool { return h.checkRunsLeft() == 0 }, 2*time.Second, 5*time.Millisecond,
		"the skipped check is over: the scheduler doesn't think it still runs")
	assert.Zero(t, h.exec.count(), "no check")
	assert.Empty(t, h.ingest.ingested(), "and no heartbeat, Down or otherwise")

	clock.Advance(3 * time.Second) // the turn is 4s away
	require.NoError(t, h.s.RunNow(context.Background(), "a"))

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("a") == 1 }, "the next tick runs")
	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "and has its heartbeat")
}

// checkRunsLeft is how many monitors the scheduler thinks are being checked
func (h *harness) checkRunsLeft() int {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return len(h.s.checks)
}
