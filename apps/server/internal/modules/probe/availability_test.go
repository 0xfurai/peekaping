package probe_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The clock these tests pin is 2026-10-20 12:00:00 UTC, so a 24h range starts on 2026-10-19 12:00
var availabilityNow = at(12, 0, 0)

func day(d, hh, mm, ss int) time.Time { return time.Date(2026, 10, d, hh, mm, ss, 0, time.UTC) }

func availabilityOn(db *bun.DB, liveness shared.LocalProbeLiveness) probe.AvailabilityService {
	svc := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo })
	return probe.NewAvailabilityService(svc, liveness, func() time.Time { return availabilityNow })
}

// change is a status change to seed
type change struct {
	status probe.Status
	at     time.Time
}

// seedHistory replaces the probe's history with the changes
func seedHistory(t *testing.T, db *bun.DB, probeID string, changes ...change) {
	t.Helper()
	_, err := db.Exec("DELETE FROM probe_status_changes WHERE probe_id = ?", probeID)
	require.NoError(t, err)
	for i, c := range changes {
		_, err := db.Exec("INSERT INTO probe_status_changes (id, probe_id, status, at, recorded_at) VALUES (?, ?, ?, ?, ?)",
			uuid.NewString(), probeID, string(c.status), c.at, c.at.Add(time.Duration(i)*time.Millisecond))
		require.NoError(t, err)
	}
}

func remoteProbe(t *testing.T, db *bun.DB) *probe.Model {
	t.Helper()
	return bareProbe(t, db, probe.NewSQLRepository(db), "Frankfurt")
}

// describedPeriods writes periods as "status start > end" in "dd hh:mm"
func describedPeriods(periods []probe.Period) []string {
	lines := make([]string, len(periods))
	for i, p := range periods {
		lines[i] = fmt.Sprintf("%s %s > %s", p.Status, p.Start.UTC().Format("02 15:04"), p.End.UTC().Format("02 15:04"))
	}
	return lines
}

func availabilityOf(t *testing.T, svc probe.AvailabilityService, probeID string, rng probe.AvailabilityRange) *probe.Availability {
	t.Helper()
	got, err := svc.Availability(context.Background(), probeID, rng)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func TestAvailability_APeriodCrossingTheRangeStartIsCutThere(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOffline, day(18, 0, 0, 0)},
			change{probe.StatusOnline, day(19, 8, 0, 0)}, // four hours before the range starts
			change{probe.StatusOffline, day(20, 6, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Equal(t, p.ID, got.ProbeID)
		assert.Equal(t, probe.Range24h, got.Range)
		assert.Equal(t, day(19, 12, 0, 0), got.From)
		assert.Equal(t, availabilityNow, got.To)
		assert.False(t, got.InProcess)
		assert.Equal(t, []string{"online 19 12:00 > 20 06:00", "offline 20 06:00 > 20 12:00"}, describedPeriods(got.Periods))
		require.NotNil(t, got.TrackedFrom)
		assert.Equal(t, day(19, 12, 0, 0), *got.TrackedFrom, "the history covers the whole range")
		require.NotNil(t, got.UptimePercent)
		assert.InDelta(t, 75.0, *got.UptimePercent, 1e-9, "18 of 24 hours")
	})
}

func TestAvailability_AProbeCreatedMidRangeCountsOnlyFromCreation(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOffline, day(20, 6, 0, 0)}, // created
			change{probe.StatusOnline, day(20, 8, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		require.NotNil(t, got.TrackedFrom)
		assert.Equal(t, day(20, 6, 0, 0), *got.TrackedFrom)
		assert.Equal(t, []string{"offline 20 06:00 > 20 08:00", "online 20 08:00 > 20 12:00"}, describedPeriods(got.Periods))
		require.NotNil(t, got.UptimePercent)
		assert.InDelta(t, 66.66666666666667, *got.UptimePercent, 1e-9, "4 of the 6 hours it has existed")
	})
}

func TestAvailability_AProbeWithNoChangeInTheRangeCarriesItsStatusIn(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		online, offline := remoteProbe(t, db), bareProbe(t, db, probe.NewSQLRepository(db), "Paris")
		seedHistory(t, db, online.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})
		seedHistory(t, db, offline.ID, change{probe.StatusOffline, day(5, 0, 0, 0)})
		svc := availabilityOn(db, shared.LocalProbeIsItsInstances)

		up := availabilityOf(t, svc, online.ID, probe.Range7d)
		down := availabilityOf(t, svc, offline.ID, probe.Range7d)

		assert.Equal(t, []string{"online 13 12:00 > 20 12:00"}, describedPeriods(up.Periods))
		require.NotNil(t, up.UptimePercent)
		assert.Equal(t, 100.0, *up.UptimePercent)
		assert.Equal(t, []string{"offline 13 12:00 > 20 12:00"}, describedPeriods(down.Periods))
		require.NotNil(t, down.UptimePercent)
		assert.Equal(t, 0.0, *down.UptimePercent)
		assert.Equal(t, day(13, 12, 0, 0), up.From, "7d is seven days")
		assert.Equal(t, day(13, 12, 0, 0), *up.TrackedFrom)
	})
}

func TestAvailability_AnOfflineGapOfTenMinutesIn24HoursIs99Point3055Percent(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOnline, day(10, 0, 0, 0)},
			change{probe.StatusOffline, day(20, 6, 0, 0)},
			change{probe.StatusOnline, day(20, 6, 10, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Equal(t, []string{"online 19 12:00 > 20 06:00", "offline 20 06:00 > 20 06:10", "online 20 06:10 > 20 12:00"}, describedPeriods(got.Periods))
		require.NotNil(t, got.UptimePercent)
		assert.InDelta(t, 99.30555555555556, *got.UptimePercent, 1e-9, "1430 of 1440 minutes")
		assert.Equal(t, got.To, got.Periods[len(got.Periods)-1].End, "the last period ends now")
	})
}

func TestAvailability_NeighbouringPeriodsOfOneStatusAreMerged(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID,
			change{probe.StatusOffline, day(10, 0, 0, 0)},
			change{probe.StatusOnline, day(19, 8, 0, 0)}, // carried into the range
			change{probe.StatusOnline, day(20, 3, 0, 0)}) // the same status again

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Equal(t, []string{"online 19 12:00 > 20 12:00"}, describedPeriods(got.Periods))
	})
}

func TestAvailability_AProbeWithNoHistoryHasNothingCovered(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID)

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range24h)

		assert.Nil(t, got.TrackedFrom)
		assert.Nil(t, got.UptimePercent)
		assert.Empty(t, got.Periods)
		assert.NotNil(t, got.Periods, "an empty list, not none")
	})
}

func TestAvailability_TheLocalProbeIsTheProcessInSingleMode(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		seedHistory(t, db, probe.LocalID, change{probe.StatusOnline, day(5, 0, 0, 0)})
		repo := probe.NewSQLRepository(db)
		hello(t, repo, probe.LocalID, "instance-1", day(20, 11, 59, 30))

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsTheProcess), probe.LocalID, probe.Range24h)

		assert.True(t, got.InProcess, "Peekaping itself checks its monitors")
		assert.Equal(t, probe.LocalID, got.ProbeID)
		assert.Empty(t, got.Periods)
		assert.NotNil(t, got.Periods)
		assert.Empty(t, got.Instances)
		assert.Nil(t, got.TrackedFrom)
		assert.Nil(t, got.UptimePercent)
		assert.Equal(t, day(19, 12, 0, 0), got.From)
	})
}

func TestAvailability_TheLocalProbeWorksLikeARemoteOneInClusterMode(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		seedHistory(t, db, probe.LocalID, change{probe.StatusOffline, day(5, 0, 0, 0)}, change{probe.StatusOnline, day(20, 0, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), probe.LocalID, probe.Range24h)

		assert.False(t, got.InProcess)
		assert.Equal(t, []string{"offline 19 12:00 > 20 00:00", "online 20 00:00 > 20 12:00"}, describedPeriods(got.Periods))
		require.NotNil(t, got.UptimePercent)
		assert.InDelta(t, 50.0, *got.UptimePercent, 1e-9)
	})
}

func TestAvailability_ARemoteProbeIsNeverInProcess(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsTheProcess), p.ID, probe.Range24h)

		assert.False(t, got.InProcess)
		assert.Len(t, got.Periods, 1)
	})
}

func TestAvailability_AnUnknownProbeIsNotFound(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		_, err := availabilityOn(db, shared.LocalProbeIsItsInstances).Availability(context.Background(), "missing", probe.Range24h)

		assert.ErrorIs(t, err, probe.ErrNotFound)
	})
}

func TestAvailability_OfAllListsEveryProbeWithoutInstances(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})
		seedHistory(t, db, probe.LocalID, change{probe.StatusOffline, day(5, 0, 0, 0)})
		hello(t, probe.NewSQLRepository(db), p.ID, "instance-1", day(20, 11, 59, 30))

		all, err := availabilityOn(db, shared.LocalProbeIsItsInstances).AvailabilityOfAll(context.Background(), probe.Range30d)

		require.NoError(t, err)
		require.Len(t, all, 2)
		assert.Equal(t, probe.LocalID, all[0].ProbeID, "the local probe comes first")
		assert.Equal(t, p.ID, all[1].ProbeID)
		assert.Equal(t, probe.Range30d, all[1].Range)
		assert.Equal(t, day(20, 12, 0, 0).AddDate(0, 0, -30), all[1].From)
		assert.Empty(t, all[1].Instances)
		require.NotNil(t, all[1].UptimePercent)
		assert.Equal(t, 100.0, *all[1].UptimePercent)
		assert.Equal(t, 0.0, *all[0].UptimePercent)
	})
}

func TestParseAvailabilityRange(t *testing.T) {
	for in, want := range map[string]probe.AvailabilityRange{"": probe.Range24h, "24h": probe.Range24h, "7d": probe.Range7d, "30d": probe.Range30d} {
		got, ok := probe.ParseAvailabilityRange(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"1h", "24H", "1y", "7 d"} {
		_, ok := probe.ParseAvailabilityRange(in)
		assert.False(t, ok, in)
	}
	assert.Equal(t, 24*time.Hour, probe.Range24h.Duration())
	assert.Equal(t, 7*24*time.Hour, probe.Range7d.Duration())
	assert.Equal(t, 30*24*time.Hour, probe.Range30d.Duration())
}

// connection is a row of probe_instance_connections to seed
func seedConnection(t *testing.T, db *bun.DB, probeID, instanceID string, start, lastSeen time.Time, ended *time.Time) {
	t.Helper()
	_, err := db.Exec("INSERT INTO probe_instance_connections (id, instance_id, probe_id, started_at, last_seen_at, ended_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.NewString(), instanceID, probeID, start, lastSeen, ended)
	require.NoError(t, err)
}

func addInstance(t *testing.T, db *bun.DB, probeID, id, hostname, version string, lastSeen time.Time) {
	t.Helper()
	require.NoError(t, probe.NewSQLRepository(db).UpsertInstance(context.Background(), probeID, &probe.Instance{ID: id, Hostname: hostname, Version: version, LastSeenAt: lastSeen}))
	_, err := db.Exec("DELETE FROM probe_instance_connections WHERE instance_id = ?", id)
	require.NoError(t, err)
}

// describedConnectionSpans writes connections as "start > end", with "(open)" for one that is
func describedSpans(connections []probe.Connection) []string {
	lines := make([]string, len(connections))
	for i, c := range connections {
		lines[i] = c.Start.UTC().Format("02 15:04:05") + " > " + c.End.UTC().Format("02 15:04:05")
		if c.Open {
			lines[i] += " (open)"
		}
	}
	return lines
}

func TestAvailability_InstancesListTheirConnectionsOverTheLast24HoursWhateverTheRange(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		p := remoteProbe(t, db)
		seedHistory(t, db, p.ID, change{probe.StatusOnline, day(5, 0, 0, 0)})
		bye := day(19, 13, 0, 0)
		recentBye := day(20, 11, 59, 50)

		// a: crosses the start of the 24 hours, ends with a bye, then connects again and is still talking
		addInstance(t, db, p.ID, "a", "host-a", "1.0.0", day(20, 11, 59, 30))
		seedConnection(t, db, p.ID, "a", day(19, 10, 0, 0), day(19, 12, 58, 0), &bye)
		seedConnection(t, db, p.ID, "a", day(20, 11, 30, 0), day(20, 11, 59, 30), nil)
		// b: went quiet, and its first connection starts before a's second, so b is listed first
		addInstance(t, db, p.ID, "b", "host-b", "1.2.0", day(19, 14, 0, 0))
		seedConnection(t, db, p.ID, "b", day(19, 12, 30, 0), day(19, 14, 0, 0), nil)
		// d: said bye ten seconds ago, while it was last seen twenty seconds ago, so it is not open
		addInstance(t, db, p.ID, "d", "host-d", "1.2.0", day(20, 11, 59, 40))
		seedConnection(t, db, p.ID, "d", day(20, 11, 0, 0), day(20, 11, 59, 40), &recentBye)
		// c: on record but silent for two days, so no connection falls in the 24 hours
		addInstance(t, db, p.ID, "c", "host-c", "0.9.0", day(18, 12, 0, 0))
		seedConnection(t, db, p.ID, "c", day(18, 11, 0, 0), day(18, 12, 0, 0), nil)

		got := availabilityOf(t, availabilityOn(db, shared.LocalProbeIsItsInstances), p.ID, probe.Range30d)

		require.Len(t, got.Instances, 4)
		ids := []string{}
		for _, instance := range got.Instances {
			ids = append(ids, instance.ID)
		}
		assert.Equal(t, []string{"a", "b", "d", "c"}, ids, "by the start of the first connection, those with none last")
		byID := map[string]probe.InstanceAvailability{}
		for _, instance := range got.Instances {
			byID[instance.ID] = instance
		}
		assert.Equal(t, "host-a", byID["a"].Hostname)
		assert.Equal(t, "1.0.0", byID["a"].Version)
		assert.Equal(t, []string{"19 12:00:00 > 19 13:00:00", "20 11:30:00 > 20 12:00:00 (open)"}, describedSpans(byID["a"].Connections),
			"cut at the start of the 24 hours, an ended one ends at its bye, an open one runs to now")
		assert.Equal(t, []string{"19 12:30:00 > 19 14:00:00"}, describedSpans(byID["b"].Connections), "a quiet one ends where it was last seen")
		assert.Equal(t, []string{"20 11:00:00 > 20 11:59:50"}, describedSpans(byID["d"].Connections), "a bye closes it even within the online window")
		assert.Equal(t, []probe.Connection{}, byID["c"].Connections)
	})
}
