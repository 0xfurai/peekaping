package producer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/worker"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimDueMonitors(t *testing.T) {
	ctx := context.Background()
	leaseMs := LeaseTTL.Milliseconds()

	t.Run("claims due monitors into the lease set, most overdue first", func(t *testing.T) {
		e := newEngine(t)
		e.addDue(t, "mon-late", t0.Add(-time.Minute))
		e.addDue(t, "mon-now", t0)
		e.addDue(t, "mon-next", t0.Add(time.Millisecond))

		ids, err := e.producer.claimDueMonitors(ctx, t0.UnixMilli(), BatchClaim, leaseMs)
		require.NoError(t, err)

		assert.Equal(t, []string{"mon-late", "mon-now"}, ids)
		assert.Equal(t, map[string]float64{"mon-next": ms(t0.Add(time.Millisecond))}, e.members(t, SchedDueKey))
		assert.Equal(t, map[string]float64{
			"mon-late": ms(t0.Add(10 * time.Second)),
			"mon-now":  ms(t0.Add(10 * time.Second)),
		}, e.members(t, SchedLeaseKey))
	})

	t.Run("claims at most the batch size", func(t *testing.T) {
		e := newEngine(t)
		for i := range BatchClaim + 10 {
			e.addDue(t, fmt.Sprintf("mon-%03d", i), t0.Add(-time.Duration(i)*time.Millisecond))
		}

		first, err := e.producer.claimDueMonitors(ctx, t0.UnixMilli(), BatchClaim, leaseMs)
		require.NoError(t, err)
		second, err := e.producer.claimDueMonitors(ctx, t0.UnixMilli(), BatchClaim, leaseMs)
		require.NoError(t, err)

		assert.Len(t, first, 50)
		assert.Len(t, second, 10)
		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Len(t, e.members(t, SchedLeaseKey), 60)
	})

	t.Run("claims nothing when no monitor is due", func(t *testing.T) {
		e := newEngine(t)
		e.addDue(t, "mon-next", t0.Add(time.Second))

		ids, err := e.producer.claimDueMonitors(ctx, t0.UnixMilli(), BatchClaim, leaseMs)
		require.NoError(t, err)

		assert.Empty(t, ids)
		assert.Empty(t, e.members(t, SchedLeaseKey))
	})
}

func TestReclaimExpiredLeases(t *testing.T) {
	e := newEngine(t)
	e.addLease(t, "mon-expired", t0.Add(-time.Second))
	e.addLease(t, "mon-expiring", t0)
	e.addLease(t, "mon-leased", t0.Add(time.Millisecond))

	ids, err := e.producer.reclaimExpiredLeases(context.Background(), t0.UnixMilli(), 5000)
	require.NoError(t, err)

	assert.Equal(t, []string{"mon-expired", "mon-expiring"}, ids)
	// Reclaimed monitors are due now, not at their missed slot
	assert.Equal(t, map[string]float64{"mon-expired": ms(t0), "mon-expiring": ms(t0)}, e.members(t, SchedDueKey))
	assert.Equal(t, map[string]float64{"mon-leased": ms(t0.Add(time.Millisecond))}, e.members(t, SchedLeaseKey))
}

func TestRunProducer_ClaimEnqueueReschedule(t *testing.T) {
	httpMon := &monitor.Model{
		ID: "mon-http", Name: "API", Type: "http", Active: true,
		Interval: 60, Timeout: 5, MaxRetries: 2, RetryInterval: 20, ResendInterval: 3,
		Config: `{"url":"https://example.com"}`,
	}
	laterMon := &monitor.Model{ID: "mon-later", Name: "Later", Type: "tcp", Active: true, Interval: 60, Timeout: 5}
	e := newEngine(t, httpMon, laterMon)
	e.addDue(t, "mon-http", t0)
	e.addDue(t, "mon-later", t0.Add(time.Second))

	e.startJobProcessing(t)

	// 12:00:30 → the check runs now, and the next one is at the next whole minute
	e.eventuallyDueAt(t, "mon-http", time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC))
	_, leased := e.score(SchedLeaseKey, "mon-http")
	assert.False(t, leased, "rescheduled monitor is still leased")

	info, payload := e.queuedCheck(t, "mon-http")
	assert.Equal(t, healthCheckQueue, info.Queue)
	assert.Equal(t, asynq.TaskStatePending, info.State)
	assert.Equal(t, 0, info.MaxRetry)
	assert.Equal(t, time.Duration(0), info.Retention)
	assert.Equal(t, worker.HealthCheckTaskTimeout(5), info.Timeout)
	assert.Equal(t, worker.HealthCheckTaskPayload{
		MonitorID:      "mon-http",
		MonitorName:    "API",
		MonitorType:    "http",
		Interval:       60,
		Timeout:        5,
		MaxRetries:     2,
		RetryInterval:  20,
		ResendInterval: 3,
		Config:         `{"url":"https://example.com"}`,
		ScheduledAt:    t0,
	}, payload)

	// mon-later was in the same claim and wasn't due yet
	score, due := e.score(SchedDueKey, "mon-later")
	assert.True(t, due)
	assert.Equal(t, ms(t0.Add(time.Second)), score)
	e.noQueuedCheck(t, "mon-later")

	// The clock jumps past mon-later's slot: it runs once, then joins the
	// minute grid instead of catching up on the slot it missed
	e.takeCheck(t, "mon-http")
	e.mr.SetTime(time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC))

	e.eventuallyDueAt(t, "mon-http", time.Date(2026, 1, 1, 12, 2, 0, 0, time.UTC))
	e.eventuallyDueAt(t, "mon-later", time.Date(2026, 1, 1, 12, 2, 0, 0, time.UTC))
	_, payload = e.queuedCheck(t, "mon-http")
	assert.Equal(t, time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC), payload.ScheduledAt)
	_, payload = e.queuedCheck(t, "mon-later")
	assert.Equal(t, time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC), payload.ScheduledAt)
}

func TestRecovery_LeaseExpiryReclaim(t *testing.T) {
	mon := &monitor.Model{ID: "mon-1", Name: "API", Type: "http", Active: true, Interval: 60, Timeout: 5}
	e := newEngine(t, mon)
	e.monitors.failNextFind("mon-1", errors.New("database is locked"))
	e.addDue(t, "mon-1", t0)

	e.startJobProcessing(t)

	// The check couldn't be built, so the monitor waits out its lease
	require.Eventually(t, func() bool {
		score, ok := e.score(SchedLeaseKey, "mon-1")
		return ok && score == ms(t0.Add(LeaseTTL))
	}, 5*time.Second, 2*time.Millisecond, "monitor never leased until 12:00:40")
	_, due := e.score(SchedDueKey, "mon-1")
	assert.False(t, due)
	e.noQueuedCheck(t, "mon-1")

	// The lease expires: the reclaimer makes it due now, and the next claim runs it
	expired := t0.Add(LeaseTTL)
	e.mr.SetTime(expired)

	e.eventuallyDueAt(t, "mon-1", time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC))
	_, payload := e.queuedCheck(t, "mon-1")
	assert.Equal(t, expired, payload.ScheduledAt)
	assert.Empty(t, e.members(t, SchedLeaseKey))
}

// PP-63: a claimed monitor that was deleted or paused is never rescheduled, but
// it isn't removed either. It stays in the lease set, and the reclaimer moves it
// back to the due set every lease period, forever.
func TestRecovery_RemovedMonitorLeavesSchedule(t *testing.T) {
	t.Skip("PP-63: deleted and paused monitors stay in the lease set")

	for name, store := range map[string][]*monitor.Model{
		"deleted": nil,
		"paused":  {{ID: "mon-1", Name: "API", Type: "http", Active: false, Interval: 60, Timeout: 5}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEngine(t, store...)
			e.addDue(t, "mon-1", t0)

			e.startJobProcessing(t)

			require.Eventually(t, func() bool {
				_, due := e.score(SchedDueKey, "mon-1")
				_, leased := e.score(SchedLeaseKey, "mon-1")
				return !due && !leased
			}, 2*time.Second, 2*time.Millisecond, "monitor is still scheduled")
			e.noQueuedCheck(t, "mon-1")
		})
	}
}
