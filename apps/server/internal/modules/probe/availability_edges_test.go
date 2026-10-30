package probe_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAvailability_AChangeAfterNowIsNotCounted(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOnline, day(10, 0, 0, 0)},
			change{probe.StatusOffline, day(20, 13, 0, 0)}) // an hour after the clock

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Equal(t, []string{"online 19 12:00 > 20 12:00"}, describedPeriods(got.Periods), "the online period is cut at now and the later change adds none")
		require.NotNil(t, got.UptimePercent)
		assert.Equal(t, 100.0, *got.UptimePercent)
	})
}

func TestAvailability_AHistoryThatStartsAfterNowCoversNothing(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(20, 13, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		require.NotNil(t, got.TrackedFrom)
		assert.Equal(t, availabilityNow, *got.TrackedFrom, "tracked from now at the earliest")
		assert.Empty(t, got.Periods)
		assert.NotNil(t, got.Periods)
		assert.Nil(t, got.UptimePercent, "no time is covered")
	})
}

func TestAvailability_TwoChangesAtTheSameMomentLeaveOnlyTheLaterOne(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOnline, day(10, 0, 0, 0)},
			change{probe.StatusOffline, day(20, 6, 0, 0)},
			change{probe.StatusOnline, day(20, 6, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Equal(t, []string{"online 19 12:00 > 20 12:00"}, describedPeriods(got.Periods), "the offline period lasted no time")
		require.NotNil(t, got.UptimePercent)
		assert.Equal(t, 100.0, *got.UptimePercent)
	})
}

// readFailingRepo is the SQL repository whose reads of the probes, their history or their
// connections fail
type readFailingRepo struct {
	probe.Repository
	findByIDErr, findAllErr, changesErr, connectionsErr error
}

func (r readFailingRepo) FindByID(ctx context.Context, id string) (*probe.Model, error) {
	if r.findByIDErr != nil {
		return nil, r.findByIDErr
	}
	return r.Repository.FindByID(ctx, id)
}

func (r readFailingRepo) FindAll(ctx context.Context) ([]*probe.Model, error) {
	if r.findAllErr != nil {
		return nil, r.findAllErr
	}
	return r.Repository.FindAll(ctx)
}

func (r readFailingRepo) FindStatusChanges(ctx context.Context, probeID string, since time.Time) ([]*probe.StatusChange, error) {
	if r.changesErr != nil {
		return nil, r.changesErr
	}
	return r.Repository.FindStatusChanges(ctx, probeID, since)
}

func (r readFailingRepo) FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*probe.InstanceConnection, error) {
	if r.connectionsErr != nil {
		return nil, r.connectionsErr
	}
	return r.Repository.FindInstanceConnections(ctx, probeID, since)
}

func availabilityWith(db *bun.DB, failing readFailingRepo) probe.AvailabilityService {
	svc := serviceOn(db, func(repo probe.Repository) probe.Repository {
		failing.Repository = repo
		return failing
	})
	return probe.NewAvailabilityService(svc, shared.LocalProbeIsItsInstances, func() time.Time { return availabilityNow })
}

func TestAvailability_AFailedReadFailsTheAnswer(t *testing.T) {
	boom := errors.New("boom")
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})

		_, err := availabilityWith(db, readFailingRepo{findByIDErr: boom}).Availability(ctx, p.ID, probe.Range24h)
		assert.ErrorIs(t, err, boom, "the probe")
		_, err = availabilityWith(db, readFailingRepo{changesErr: boom}).Availability(ctx, p.ID, probe.Range24h)
		assert.ErrorIs(t, err, boom, "the history")
		_, err = availabilityWith(db, readFailingRepo{connectionsErr: boom}).Availability(ctx, p.ID, probe.Range24h)
		assert.ErrorIs(t, err, boom, "the connections")
		_, err = availabilityWith(db, readFailingRepo{findAllErr: boom}).AvailabilityOfAll(ctx, probe.Range24h)
		assert.ErrorIs(t, err, boom, "the probes")
		_, err = availabilityWith(db, readFailingRepo{changesErr: boom}).AvailabilityOfAll(ctx, probe.Range24h)
		assert.ErrorIs(t, err, boom, "the history in the list")
	})
}

func TestAvailability_InstancesWithNoConnectionAreOrderedByIDAfterThoseWithOne(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})
		longEnded := day(15, 13, 0, 0)

		// a and b have no connection in the last 24 hours (a's ended days before the window)
		addInstance(t, db, p.ID, "a", "host-a", "1.0.0", day(15, 13, 0, 0))
		seedConnection(t, db, p.ID, "a", day(15, 10, 0, 0), day(15, 13, 0, 0), &longEnded)
		addInstance(t, db, p.ID, "b", "host-b", "1.0.0", day(15, 13, 0, 0))
		// c and d connected at the same moment, e later, f earlier
		addInstance(t, db, p.ID, "c", "host-c", "1.0.0", day(20, 11, 59, 30))
		seedConnection(t, db, p.ID, "c", day(20, 11, 0, 0), day(20, 11, 59, 30), nil)
		addInstance(t, db, p.ID, "d", "host-d", "1.0.0", day(20, 11, 59, 30))
		seedConnection(t, db, p.ID, "d", day(20, 11, 0, 0), day(20, 11, 59, 30), nil)
		addInstance(t, db, p.ID, "e", "host-e", "1.0.0", day(20, 11, 59, 30))
		seedConnection(t, db, p.ID, "e", day(20, 11, 30, 0), day(20, 11, 59, 30), nil)
		addInstance(t, db, p.ID, "f", "host-f", "1.0.0", day(20, 11, 59, 30))
		seedConnection(t, db, p.ID, "f", day(20, 10, 0, 0), day(20, 11, 59, 30), nil)

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		ids := []string{}
		for _, instance := range got.Instances {
			ids = append(ids, instance.ID)
			if instance.ID == "a" || instance.ID == "b" {
				assert.Equal(t, []probe.Connection{}, instance.Connections, instance.ID)
			}
		}
		assert.Equal(t, []string{"f", "c", "d", "e", "a", "b"}, ids)
	})
}
