package scheduler

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestScheduler_RunNowChecksAMonitorRightAway(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")

	require.NoError(t, h.s.RunNow(context.Background(), "mon"))

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 2 }, "the monitor is checked again right away")
	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 2 }, "and the check is ingested")
}

func TestScheduler_RunNowSkipsAMonitorWhoseCheckIsRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	tokens := h.holdChecks()
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

	require.NoError(t, h.s.RunNow(context.Background(), "mon"))

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("mon"), "a monitor runs one check at a time")
	close(tokens)
	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "the running check is ingested")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("mon"), "the skipped check doesn't run later")
}

func TestScheduler_RunNowPutsAMonitorThatLostItsTimerBackOnItsSchedule(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.start()
	// Created without an event reaching the scheduler
	h.db.put(monitorWith("mon", withInterval(1)))
	sleepUntilSecondFraction(500 * time.Millisecond)
	askedAt := time.Now()

	require.NoError(t, h.s.RunNow(context.Background(), "mon"))

	eventually(t, 3*time.Second, func() bool { return h.exec.countFor("mon") >= 3 }, "the monitor is checked now and on its interval")
	calls := h.exec.callsFor("mon")
	assert.Less(t, calls[0].at.Sub(askedAt), lateness, "the first check is right away")
	assert.Less(t, intoSecond(calls[1].at), lateness, "the next check is at the start of a second")
	assert.Less(t, intoSecond(calls[2].at), lateness, "and so is the one after")
}

func TestScheduler_RunNowFailsForAMonitorItCannotCheck(t *testing.T) {
	t.Parallel()
	t.Run("before the scheduler starts", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.db.put(monitorWith("mon", withInterval(day)))

		assert.Error(t, h.s.RunNow(context.Background(), "mon"))
		time.Sleep(100 * time.Millisecond)
		assert.Zero(t, h.exec.count())
	})
	t.Run("a monitor that doesn't exist", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.start()

		assert.Error(t, h.s.RunNow(context.Background(), "gone"))
	})
	t.Run("a paused monitor", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.db.put(monitorWith("mon", withInterval(day), paused))
		h.start()

		assert.Error(t, h.s.RunNow(context.Background(), "mon"))
		time.Sleep(100 * time.Millisecond)
		assert.Zero(t, h.exec.count())
	})
}

// heartbeatsOf finds a monitor's latest heartbeat among the results the ingester stored
type heartbeatsOf struct{ ingest *recordingIngester }

func (h heartbeatsOf) FindByMonitorIDPaginated(_ context.Context, monitorID string, _, _ int, _ *bool, _ bool) ([]*shared.HeartBeatModel, error) {
	results := h.ingest.ingested()
	for i := len(results) - 1; i >= 0; i-- {
		if r := results[i]; r.MonitorID == monitorID {
			return []*shared.HeartBeatModel{{MonitorID: monitorID, Status: r.Status, Time: r.StartTime}}, nil
		}
	}
	return nil, nil
}

func TestWatchdog_GetsAStaleMonitorCheckedAgainByTheScheduler(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day), func(m *shared.Monitor) { m.UpdatedAt = time.Now().Add(-4 * 24 * time.Hour) }))
	// The first check reports nothing, so the monitor has gone 4 days without a heartbeat
	h.exec.run = func(_ context.Context, call checkCall) *executor.Result {
		if call.n == 1 {
			return nil
		}
		return up("checked again")
	}
	store := stale.NewMemoryStore()
	watchdog := stale.NewWatchdog(stale.WatchdogConfig{
		Store:        store,
		Rechecker:    h.s,
		Monitors:     h.db,
		Heartbeats:   heartbeatsOf{h.ingest},
		EventBus:     h.bus,
		CheckTimeout: func(int) time.Duration { return time.Hour },
		Interval:     200 * time.Millisecond,
		Logger:       zap.NewNop().Sugar(),
	})
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)

	go watchdog.Run(ctx)

	stored := func() map[string]stale.Entry {
		entries, err := store.All(context.Background())
		require.NoError(t, err)
		return entries
	}
	eventually(t, 2*time.Second, func() bool { _, ok := stored()["mon"]; return ok }, "the watchdog records the stale monitor in memory")
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 2 }, "and asks the scheduler to check it again")
	eventually(t, 2*time.Second, func() bool { return len(stored()) == 0 }, "the check's heartbeat ends its stale record")
	assert.Equal(t, []string{"checked again"}, h.ingest.messages())
}
