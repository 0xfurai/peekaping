package supervisor

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// spacedSupervisor is a supervisor over the real registry, whose spacer runs on a clock the test
// moves and sleeps by recording what it was asked to wait
type spacedSupervisor struct {
	*HealthCheckSupervisor
	clock *fakeClock
	slept []time.Duration
	logs  *observer.ObservedLogs
}

func newSpacedSupervisor(t *testing.T, perMinute int) *spacedSupervisor {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	s := &spacedSupervisor{
		HealthCheckSupervisor: NewHealthCheck(nil, executor.NewExecutorRegistry(zap.NewNop().Sugar()), zap.New(core).Sugar()),
		clock:                 newFakeClock(),
		logs:                  logs,
	}
	s.spacer = newHostSpacer(perMinute, s.clock.Now)
	s.spacer.sleep = func(_ context.Context, d time.Duration) error {
		s.slept = append(s.slept, d)
		return nil
	}
	return s
}

func tcpMonitor(host string, interval int) *shared.Monitor {
	return &shared.Monitor{
		ID: "mon-" + host, Name: host, Type: "tcp", Interval: interval, Timeout: 5,
		Config: `{"host":"` + host + `","port":80}`,
	}
}

func upExecutor() *stubExecutor {
	return &stubExecutor{result: &executor.Result{Status: shared.MonitorStatusUp, Message: "ok"}}
}

// countingExecutor counts its checks
type countingExecutor struct {
	stubExecutor
	runs int
}

func (e *countingExecutor) Execute(ctx context.Context, m *shared.Monitor, p *executor.Proxy) *executor.Result {
	e.runs++
	return e.stubExecutor.Execute(ctx, m, p)
}

func TestHandleMonitorTick_ACheckWhoseTurnIsNearWaitsForItAndThenRuns(t *testing.T) {
	s := newSpacedSupervisor(t, 60)
	exec := &countingExecutor{stubExecutor: *upExecutor()}
	m := tcpMonitor("example.com", 60)

	first := s.HandleMonitorTick(context.Background(), m, exec, nil, false)
	second := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 60), exec, nil, false)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Equal(t, 2, exec.runs)
	assert.Equal(t, []time.Duration{time.Second}, s.slept, "only the second waited, a second for its turn")
}

func TestHandleMonitorTick_ACheckWhoseTurnIsFarSkipsItsTickWithoutAResult(t *testing.T) {
	s := newSpacedSupervisor(t, 60)
	exec := &countingExecutor{stubExecutor: *upExecutor()}
	// An interval of 20s lets a check wait 5s
	for range 6 {
		require.NotNil(t, s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false))
	}
	require.Equal(t, 6, exec.runs)

	tick := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false)

	assert.Nil(t, tick, "no result, so no heartbeat and no Down")
	assert.Equal(t, 6, exec.runs, "and no check")
	skips := s.logs.FilterMessage("Check skipped: its target host is at the checks-per-host limit").All()
	require.Len(t, skips, 1)
	assert.Equal(t, zap.DebugLevel, skips[0].Level)
	assert.Equal(t, "example.com", skips[0].ContextMap()["host"])

	s.clock.Advance(time.Second)
	next := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false)
	assert.NotNil(t, next, "the next tick runs: the skipped one took no turn")
	assert.Equal(t, 7, exec.runs)
}

func TestHandleMonitorTick_AMonitorMayWaitAQuarterOfItsIntervalAtMostFiveSeconds(t *testing.T) {
	tests := []struct {
		interval int
		// turns taken before, each a second further on than the last, and the one that is tried
		taken    int
		wantRuns bool
	}{
		{interval: 8, taken: 2, wantRuns: true},   // waits 2s, may wait 2s
		{interval: 8, taken: 3, wantRuns: false},  // waits 3s, may wait 2s
		{interval: 600, taken: 5, wantRuns: true}, // waits 5s, may wait at most 5s
		{interval: 600, taken: 6, wantRuns: false},
	}
	for _, tc := range tests {
		s := newSpacedSupervisor(t, 60)
		exec := &countingExecutor{stubExecutor: *upExecutor()}
		for range tc.taken {
			s.spacer.Reserve([]string{"example.com"}, time.Hour)
		}

		tick := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", tc.interval), exec, nil, false)

		assert.Equal(t, tc.wantRuns, tick != nil, "interval %ds, %d turns taken", tc.interval, tc.taken)
	}
}

func TestHandleMonitorTick_AMonitorWithoutAHostIsNeverLimited(t *testing.T) {
	s := newSpacedSupervisor(t, 1)
	exec := &countingExecutor{stubExecutor: *upExecutor()}
	push := &shared.Monitor{ID: "p", Name: "p", Type: "push", Interval: 20, Timeout: 5, Config: `{}`}
	broken := &shared.Monitor{ID: "b", Name: "b", Type: "tcp", Interval: 20, Timeout: 5, Config: `not json`}

	for range 5 {
		require.NotNil(t, s.HandleMonitorTick(context.Background(), push, exec, nil, false))
		require.NotNil(t, s.HandleMonitorTick(context.Background(), broken, exec, nil, false))
	}

	assert.Equal(t, 10, exec.runs)
	assert.Empty(t, s.slept)
	assert.Zero(t, s.spacer.tracked())
}

func TestHandleMonitorTick_AMonitorUnderMaintenanceTakesNoTurn(t *testing.T) {
	s := newSpacedSupervisor(t, 60)
	exec := &countingExecutor{stubExecutor: *upExecutor()}

	for range 10 {
		tick := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, true)
		require.NotNil(t, tick)
		assert.True(t, tick.IsUnderMaintenance)
	}

	assert.Zero(t, exec.runs)
	assert.Zero(t, s.spacer.tracked())
}

func TestHandleMonitorTick_WithTheSpacerOffNothingWaitsOrSkips(t *testing.T) {
	s := newSpacedSupervisor(t, 0)
	exec := &countingExecutor{stubExecutor: *upExecutor()}

	for range 50 {
		require.NotNil(t, s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false))
	}

	assert.Equal(t, 50, exec.runs)
	assert.Empty(t, s.slept)
}

func TestHandleMonitorTick_ACheckCancelledWhileItWaitsGivesItsTurnBack(t *testing.T) {
	s := newSpacedSupervisor(t, 60)
	exec := &countingExecutor{stubExecutor: *upExecutor()}
	require.NotNil(t, s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false))
	s.spacer.sleep = func(ctx context.Context, d time.Duration) error { return context.Canceled }

	tick := s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false)

	assert.Nil(t, tick)
	assert.Equal(t, 1, exec.runs)
	turn := s.spacer.Reserve([]string{"example.com"}, time.Minute)
	assert.Equal(t, time.Second, turn.Wait, "the cancelled check's turn is free again")
}

func TestHandleMonitorTick_ReportsTheSkippedChecksOfAHostAtInfoOncePerMinute(t *testing.T) {
	s := newSpacedSupervisor(t, 60)
	exec := &countingExecutor{stubExecutor: *upExecutor()}
	for range 6 {
		s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false)
	}

	for range 4 {
		assert.Nil(t, s.HandleMonitorTick(context.Background(), tcpMonitor("example.com", 20), exec, nil, false))
	}

	infos := s.logs.FilterLevelExact(zap.InfoLevel).All()
	require.Len(t, infos, 1, "four skips, one line")
	assert.Equal(t, "example.com", infos[0].ContextMap()["host"])
	assert.EqualValues(t, 1, infos[0].ContextMap()["skipped"], "it comes with the first skip")
	assert.Len(t, s.logs.FilterLevelExact(zap.DebugLevel).FilterMessage("Check skipped: its target host is at the checks-per-host limit").All(), 4)
}

func TestHealthCheckSupervisor_TheCapCanBeChangedAtRunTime(t *testing.T) {
	s := NewHealthCheck(nil, executor.NewExecutorRegistry(zap.NewNop().Sugar()), zap.NewNop().Sugar())
	assert.Zero(t, s.ChecksPerHostPerMinute(), "off unless set")

	s.SetChecksPerHostPerMinute(90)
	assert.Equal(t, 90, s.ChecksPerHostPerMinute())

	s.SetChecksPerHostPerMinute(0)
	assert.Zero(t, s.ChecksPerHostPerMinute())
}
