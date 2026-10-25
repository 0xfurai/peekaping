package stats_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// onEverySQLBackend runs fn with a stats service over a migrated database and a monitor in it.
func onEverySQLBackend(t *testing.T, fn func(t *testing.T, svc stats.Service, monitorID string)) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			db := backend.open(t)
			migrateUp(t, db, migrations.Migrations)
			fn(t, stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar()), insertMonitor(t, db))
		})
	}
}

// probeChecks records checks from two probes in two minutes:
//
//	minute 0: local Up 100 ms, frankfurt Up 300 ms, frankfurt Down
//	minute 1: frankfurt Up 500 ms
func probeChecks(t *testing.T, svc stats.Service, monitorID string, minute time.Time) {
	t.Helper()
	aggregateFrom(t, svc, monitorID, "local", shared.MonitorStatusUp, 100, minute.Add(10*time.Second))
	aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusUp, 300, minute.Add(20*time.Second))
	aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusDown, 0, minute.Add(30*time.Second))
	aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusUp, 500, minute.Add(time.Minute+5*time.Second))
}

func TestFindStatsByMonitorIDAndTimeRangeFiltersByProbe(t *testing.T) {
	onEverySQLBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		ctx := context.Background()
		minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		probeChecks(t, svc, monitorID, minute)

		type row struct {
			ProbeID string
			Bucket  bucket
		}
		read := func(probeID string) []row {
			t.Helper()
			stored, err := svc.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, probeID, minute, minute.Add(time.Minute), stats.StatMinutely)
			require.NoError(t, err)
			var rows []row
			for _, s := range stored {
				rows = append(rows, row{s.ProbeID, bucket{s.Up, s.Down, s.Maintenance, s.Ping, s.PingMin, s.PingMax}})
			}
			return rows
		}

		assert.Equal(t, []row{
			{"", bucket{Up: 2, Down: 1, Ping: 200, PingMin: 100, PingMax: 300}},
			{"", bucket{Up: 1, Ping: 500, PingMin: 500, PingMax: 500}},
		}, read(""), "without a probe: every probe's checks add up")
		assert.Equal(t, []row{
			{"local", bucket{Up: 1, Ping: 100, PingMin: 100, PingMax: 100}},
		}, read("local"))
		assert.Equal(t, []row{
			{"probe-frankfurt", bucket{Up: 1, Down: 1, Ping: 300, PingMin: 300, PingMax: 300}},
			{"probe-frankfurt", bucket{Up: 1, Ping: 500, PingMin: 500, PingMax: 500}},
		}, read("probe-frankfurt"))
		assert.Empty(t, read("probe-oslo"), "a probe that ran no checks of the monitor")
	})
}

func TestSummarizeFiltersByProbe(t *testing.T) {
	onEverySQLBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		ctx := context.Background()
		minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		probeChecks(t, svc, monitorID, minute)

		summarize := func(probeID string) (uptime, avgPing float64) {
			t.Helper()
			summary, err := svc.Summarize(ctx, monitorID, probeID, minute, minute.Add(time.Minute))
			require.NoError(t, err)
			require.NotNil(t, summary.Uptime, probeID)
			require.NotNil(t, summary.AvgPing, probeID)
			return *summary.Uptime, *summary.AvgPing
		}

		uptime, avgPing := summarize("")
		assert.Equal(t, 75.0, uptime, "3 of 4 checks were Up")
		assert.Equal(t, 300.0, avgPing)
		uptime, avgPing = summarize("local")
		assert.Equal(t, 100.0, uptime)
		assert.Equal(t, 100.0, avgPing)
		uptime, avgPing = summarize("probe-frankfurt")
		assert.InDelta(t, 66.6667, uptime, 1e-4, "2 of 3 checks were Up")
		assert.Equal(t, 400.0, avgPing)

		none, err := svc.Summarize(ctx, monitorID, "probe-oslo", minute, minute.Add(time.Minute))
		require.NoError(t, err)
		assert.Nil(t, none.Uptime)
		assert.Nil(t, none.AvgPing)
	})
}

func TestFindStatsWithIntervalFiltersByProbe(t *testing.T) {
	onEverySQLBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		ctx := context.Background()
		minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		probeChecks(t, svc, monitorID, minute)

		// A monitor checked every 2 minutes groups its minute stats in twos
		grouped := func(probeID string) []bucket {
			t.Helper()
			stored, err := svc.FindStatsByMonitorIDAndTimeRangeWithInterval(ctx, monitorID, probeID, minute, minute.Add(2*time.Minute), stats.StatMinutely, 120)
			require.NoError(t, err)
			var buckets []bucket
			for _, s := range stored {
				buckets = append(buckets, bucket{s.Up, s.Down, s.Maintenance, s.Ping, s.PingMin, s.PingMax})
			}
			return buckets
		}

		assert.Equal(t, []bucket{{Up: 3, Down: 1, Ping: 300, PingMin: 100, PingMax: 500}}, grouped(""))
		assert.Equal(t, []bucket{{Up: 1, Ping: 100, PingMin: 100, PingMax: 100}}, grouped("local"))
		assert.Equal(t, []bucket{{Up: 2, Down: 1, Ping: 400, PingMin: 300, PingMax: 500}}, grouped("probe-frankfurt"))
	})
}
