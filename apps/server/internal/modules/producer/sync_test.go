package producer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// activeMonitors returns n active 60s monitors, mon-000 to mon-<n-1>.
func activeMonitors(n int) []*monitor.Model {
	monitors := make([]*monitor.Model, n)
	for i := range monitors {
		monitors[i] = &monitor.Model{ID: fmt.Sprintf("mon-%03d", i), Type: "http", Active: true, Interval: 60}
	}
	return monitors
}

func TestSync_SchedulesEveryPage(t *testing.T) {
	for name, sync := range map[string]func(*Producer) error{
		"startup": (*Producer).initializeSchedule,
		"refresh": (*Producer).refreshSchedule,
	} {
		t.Run(name, func(t *testing.T) {
			// 150 monitors are two pages of 100
			e := newEngine(t, activeMonitors(150)...)

			require.NoError(t, sync(e.producer))

			due := e.members(t, SchedDueKey)
			assert.Len(t, due, 150)
			for _, id := range []string{"mon-000", "mon-049", "mon-050", "mon-149"} {
				assert.Equal(t, ms(t0), due[id], id)
				assert.Equal(t, 60, e.producer.monitorIntervals[id], id)
			}
		})
	}
}

func TestInitializeSchedule_KeepsTheExistingSchedule(t *testing.T) {
	e := newEngine(t,
		&monitor.Model{ID: "mon-due", Active: true, Interval: 60},
		&monitor.Model{ID: "mon-leased", Active: true, Interval: 60},
		&monitor.Model{ID: "mon-new", Active: true, Interval: 60},
		&monitor.Model{ID: "mon-paused", Active: false, Interval: 60},
	)
	slot := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	e.addDue(t, "mon-due", slot)
	e.addLease(t, "mon-leased", t0.Add(LeaseTTL))
	e.addDue(t, "mon-paused", slot)
	e.addLease(t, "mon-deleted", t0.Add(LeaseTTL))

	require.NoError(t, e.producer.initializeSchedule())

	// A scheduled monitor keeps its slot, and a leased one stays with the
	// producer that claimed it. A new one is due now.
	assert.Equal(t, map[string]float64{
		"mon-due": ms(slot),
		"mon-new": ms(t0),
	}, e.members(t, SchedDueKey))
	assert.Equal(t, map[string]float64{"mon-leased": ms(t0.Add(LeaseTTL))}, e.members(t, SchedLeaseKey))
	assert.Equal(t, map[string]int{"mon-due": 60, "mon-leased": 60, "mon-new": 60}, e.producer.monitorIntervals)
}

func TestRefreshSchedule_FollowsTheDatabase(t *testing.T) {
	e := newEngine(t,
		&monitor.Model{ID: "mon-same", Active: true, Interval: 60},
		&monitor.Model{ID: "mon-changed", Active: true, Interval: 120},
		&monitor.Model{ID: "mon-new", Active: true, Interval: 30},
		&monitor.Model{ID: "mon-paused", Active: false, Interval: 60},
	)
	slot := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	e.producer.monitorIntervals = map[string]int{"mon-same": 60, "mon-changed": 60, "mon-paused": 60, "mon-deleted": 60}
	e.addDue(t, "mon-same", slot)
	e.addLease(t, "mon-changed", t0.Add(LeaseTTL))
	e.addDue(t, "mon-paused", slot)
	e.addLease(t, "mon-deleted", t0.Add(LeaseTTL))

	require.NoError(t, e.producer.refreshSchedule())

	assert.Equal(t, map[string]float64{
		"mon-same":    ms(slot),                                         // unchanged, keeps its slot
		"mon-changed": ms(time.Date(2026, 1, 1, 12, 2, 0, 0, time.UTC)), // next 2-minute slot after 12:00:30
		"mon-new":     ms(t0),                                           // due now
	}, e.members(t, SchedDueKey))
	assert.Empty(t, e.members(t, SchedLeaseKey))
	assert.Equal(t, map[string]int{"mon-same": 60, "mon-changed": 120, "mon-new": 30}, e.producer.monitorIntervals)
}

func TestRunScheduleRefresher(t *testing.T) {
	e := newEngine(t)
	p := e.producer
	p.scheduleRefreshInterval = 5 * time.Millisecond
	syncCtx, syncCancel := context.WithCancel(p.ctx)
	defer syncCancel()
	p.wg.Add(1)
	go p.runScheduleRefresher(syncCtx)
	t.Cleanup(p.Stop)

	// A monitor that reached the database without an event shows up on the next refresh
	e.monitors.put(&monitor.Model{ID: "mon-new", Active: true, Interval: 60})
	e.eventuallyDueAt(t, "mon-new", t0)

	// When syncing stops, the refresher exits while the producer runs on
	syncCancel()
	waitForGoroutines(t, &p.wg)
}

// PP-63: sync pages through active monitors by offset. Pausing a monitor on a
// page that's already read shifts the next page by one, so the first monitor
// of that page is never seen, and sync removes it from the schedule.
func TestSync_PausingAMonitorMidSyncKeepsTheOthers(t *testing.T) {
	t.Skip("PP-63: offset paging drops an active monitor when another is paused mid-sync")

	for name, sync := range map[string]func(*Producer) error{
		"startup": (*Producer).initializeSchedule,
		"refresh": (*Producer).refreshSchedule,
	} {
		t.Run(name, func(t *testing.T) {
			// Highest ID first: page 0 is mon-149..mon-050, page 1 is mon-049..mon-000
			e := newEngine(t, activeMonitors(150)...)
			slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
			for i := range 150 {
				id := fmt.Sprintf("mon-%03d", i)
				e.addDue(t, id, slot)
				e.producer.monitorIntervals[id] = 60
			}
			e.monitors.afterPage = func(page int) {
				if page == 0 {
					e.monitors.setActive("mon-100", false)
				}
			}

			require.NoError(t, sync(e.producer))

			score, due := e.score(SchedDueKey, "mon-049")
			assert.True(t, due, "mon-049 was dropped from the schedule")
			assert.Equal(t, ms(slot), score)
		})
	}
}
