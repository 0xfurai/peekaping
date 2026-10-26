package modules_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.uber.org/zap"
)

// knownProbes is a probe lookup that knows the probes with these ids, whatever they run
type knownProbes map[string]bool

func (k knownProbes) FindProbe(_ context.Context, id string) (*monitor.ProbeInfo, error) {
	if !k[id] {
		return nil, nil
	}
	return &monitor.ProbeInfo{Name: id}, nil
}

// assignFixture is a monitor service and probe repository over one database, with a probe that
// waits to be assigned
type assignFixture struct {
	db       *bun.DB
	service  monitor.Service
	probes   probe.Repository
	bus      *eventstest.Bus
	probeID  string
	claimIt  func(ctx context.Context) (bool, error)
	monitors map[string]string
}

func newAssignFixture(t *testing.T, ctx context.Context, db *bun.DB) *assignFixture {
	t.Helper()
	logger := zap.NewNop().Sugar()
	bus := eventstest.NewBus()
	probes := probe.NewSQLRepository(db)
	waiting, err := probes.Create(ctx, &probe.CreateModel{Name: "Frankfurt", TokenHash: "sha256$" + uuid.NewString(), TokenPrefix: "pk_", AssignAllPending: true})
	require.NoError(t, err)
	service := monitor.NewMonitorService(monitor.NewSQLRepository(db), dbtx.NewSQLTransactor(db),
		heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, logger), bus,
		monitor_notification.NewService(monitor_notification.NewSQLRepository(db), logger),
		monitor_tag.NewService(monitor_tag.NewSQLRepository(db), logger),
		nil, stats.NewService(stats.NewSQLRepository(db), logger), knownProbes{"local": true, waiting.ID: true}, logger)
	return &assignFixture{
		db: db, service: service, probes: probes, bus: bus, probeID: waiting.ID, monitors: map[string]string{},
		claimIt: func(ctx context.Context) (bool, error) { return probes.ClaimPendingAssignment(ctx, waiting.ID) },
	}
}

// insert stores a monitor with no probe rows, as a monitor from before the probes is
func (f *assignFixture) insert(t *testing.T, ctx context.Context, monitorType string, active bool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.db.ExecContext(ctx,
		`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active, config)
		 VALUES (?, ?, ?, 60, 16, 0, 60, 0, ?, '{}')`, id, monitorType, monitorType, active)
	require.NoError(t, err)
	return id
}

func (f *assignFixture) probesOf(t *testing.T, ctx context.Context, monitorID string) []string {
	t.Helper()
	ids := []string{}
	require.NoError(t, f.db.NewSelect().Table("monitor_probes").Column("probe_id").Where("monitor_id = ?", monitorID).Scan(ctx, &ids))
	return ids
}

func (f *assignFixture) pending(t *testing.T, ctx context.Context) bool {
	t.Helper()
	found, err := f.probes.FindByID(ctx, f.probeID)
	require.NoError(t, err)
	require.NotNil(t, found)
	return found.AssignAllPending
}

// AC2, AC4: the probe is added to the monitors it can run, in more than one chunk, and each changed
// monitor is announced after the commit
func TestMonitorService_AssignProbeToAllOnEverySQLBackend(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		f := newAssignFixture(t, ctx, db)
		const many = 1100 // more than two chunks of 500
		wanted := make([]string, 0, many)
		for range many {
			wanted = append(wanted, f.insert(t, ctx, "http", true))
		}
		paused := f.insert(t, ctx, "tcp", false)
		docker := f.insert(t, ctx, "docker", true)
		push := f.insert(t, ctx, "push", true)
		wanted = append(wanted, paused)

		added, err := f.service.AssignProbeToAll(ctx, f.probeID, []string{"http", "tcp"}, f.claimIt)

		require.NoError(t, err)
		assert.Equal(t, many+1, added)
		assert.False(t, f.pending(t, ctx), "the claim cleared the flag")
		for _, id := range []string{wanted[0], wanted[many-1], paused} {
			assert.ElementsMatch(t, []string{"local", f.probeID}, f.probesOf(t, ctx, id), "a monitor with no rows keeps the local probe")
		}
		assert.Empty(t, f.probesOf(t, ctx, docker), "a type the instance doesn't run")
		assert.Empty(t, f.probesOf(t, ctx, push))
		count, err := db.NewSelect().Table("monitor_probes").Where("probe_id = ?", f.probeID).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, many+1, count)

		announced := eventstest.Published(f.bus, events.MonitorUpdated)
		assert.Len(t, announced, many+1, "one MonitorUpdated for each changed monitor")
		ids := make([]string, len(announced))
		for i, m := range announced {
			ids[i] = m.ID
			assert.Contains(t, m.ProbeIDs, f.probeID)
		}
		assert.ElementsMatch(t, wanted, ids)

		again, err := f.service.AssignProbeToAll(ctx, f.probeID, nil, f.claimIt)
		require.NoError(t, err)
		assert.Zero(t, again, "the claim is spent")
		assert.Len(t, eventstest.Published(f.bus, events.MonitorUpdated), many+1)
	})
}

// With no claim, as the action passes none, the probe is added to the monitors it isn't on yet and
// the types of every kind are taken for an empty list
func TestMonitorService_AssignProbeToAllWithoutAClaim(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		f := newAssignFixture(t, ctx, db)
		first, second := f.insert(t, ctx, "http", true), f.insert(t, ctx, "docker", true)

		added, err := f.service.AssignProbeToAll(ctx, f.probeID, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, added)
		assert.True(t, f.pending(t, ctx), "no claim, so the flag is not touched")

		later := f.insert(t, ctx, "tcp", true)
		added, err = f.service.AssignProbeToAll(ctx, f.probeID, []string{}, nil)
		require.NoError(t, err)
		assert.Equal(t, 1, added, "only the monitor that isn't on the probe yet")
		assert.ElementsMatch(t, []string{"local", f.probeID}, f.probesOf(t, ctx, later))
		assert.ElementsMatch(t, []string{"local", f.probeID}, f.probesOf(t, ctx, first))
		assert.ElementsMatch(t, []string{"local", f.probeID}, f.probesOf(t, ctx, second))
	})
}

// AC3: a failed assignment rolls the claim back, so that the flag stays and the next one assigns
func TestMonitorService_AssignProbeToAllRollsTheClaimBackOnFailure(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		f := newAssignFixture(t, ctx, db)
		id := f.insert(t, ctx, "http", true)

		added, err := f.service.AssignProbeToAll(ctx, uuid.NewString(), nil, func(ctx context.Context) (bool, error) {
			return f.claimIt(ctx)
		})
		require.Error(t, err, "an unknown probe")
		assert.Zero(t, added)
		assert.True(t, f.pending(t, ctx), "the claim, made before the failure, is rolled back")
		assert.Empty(t, f.probesOf(t, ctx, id))
		assert.Empty(t, eventstest.Published(f.bus, events.MonitorUpdated))

		_, err = f.service.AssignProbeToAll(ctx, f.probeID, nil, func(context.Context) (bool, error) { return false, errors.New("claim failed") })
		require.Error(t, err)
		assert.Empty(t, f.probesOf(t, ctx, id))

		added, err = f.service.AssignProbeToAll(ctx, f.probeID, nil, f.claimIt)
		require.NoError(t, err)
		assert.Equal(t, 1, added)
	})
}

// AC3: of callers that race to assign a probe that waits, one assigns. On PostgreSQL the callers
// run on connections of their own, and the later claim waits for the first to commit.
func TestMonitorService_OfTwoRacingAssignmentsOneAssigns(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		if db.Dialect().Name() == dialect.PG {
			db.SetMaxOpenConns(8)
		}
		f := newAssignFixture(t, ctx, db)
		const monitors = 30
		for range monitors {
			f.insert(t, ctx, "http", true)
		}

		const callers = 6
		var wg sync.WaitGroup
		results := make([]int, callers)
		errs := make([]error, callers)
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = f.service.AssignProbeToAll(ctx, f.probeID, nil, f.claimIt)
			}()
		}
		wg.Wait()

		for _, err := range errs {
			require.NoError(t, err)
		}
		total, winners := 0, 0
		for _, added := range results {
			total += added
			if added > 0 {
				winners++
			}
		}
		assert.Equal(t, monitors, total, "every monitor is assigned once")
		assert.Equal(t, 1, winners, "by one caller")
		assert.Len(t, eventstest.Published(f.bus, events.MonitorUpdated), monitors)
		assert.False(t, f.pending(t, ctx))
	})
}

// The probe repository's claim and setter, which the service and the hello use
func TestProbeRepository_ClaimPendingAssignmentClearsTheFlagOnce(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		f := newAssignFixture(t, ctx, db)

		claimed, err := f.probes.ClaimPendingAssignment(ctx, f.probeID)
		require.NoError(t, err)
		assert.True(t, claimed)
		claimed, err = f.probes.ClaimPendingAssignment(ctx, f.probeID)
		require.NoError(t, err)
		assert.False(t, claimed, "the flag was cleared")
		claimed, err = f.probes.ClaimPendingAssignment(ctx, "local")
		require.NoError(t, err)
		assert.False(t, claimed, "a probe that doesn't wait")

		require.NoError(t, f.probes.SetPendingAssignment(ctx, f.probeID))
		assert.True(t, f.pending(t, ctx))

		err = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			claimed, err := f.probes.ClaimPendingAssignment(ctx, f.probeID)
			require.NoError(t, err)
			require.True(t, claimed)
			return errRollBack
		})
		require.ErrorIs(t, err, errRollBack)
		assert.True(t, f.pending(t, ctx), "a claim in a transaction that rolls back leaves the flag set")
	})
}
