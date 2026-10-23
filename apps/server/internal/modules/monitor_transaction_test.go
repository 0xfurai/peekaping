package modules_test

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// failingStats is the stats service with a delete that always fails.
type failingStats struct {
	stats.Service
}

func (failingStats) DeleteByMonitorID(context.Context, string) error {
	return errors.New("stats are locked")
}

// The monitor service writes a monitor and its links in one transaction on SQLite and on
// PostgreSQL, where a failed statement aborts the transaction. Each of these checks fails the
// write at its last step, so that the earlier steps have already run.
func TestMonitorService_WritesInOneTransactionOnEverySQLBackend(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		logger := zap.NewNop().Sugar()
		bus := eventstest.NewBus()
		tags := monitor_tag.NewService(monitor_tag.NewSQLRepository(db), logger)
		channels := monitor_notification.NewService(monitor_notification.NewSQLRepository(db), logger)
		heartbeats := heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, logger)
		statService := stats.NewService(stats.NewSQLRepository(db), logger)
		newService := func(statService stats.Service) monitor.Service {
			return monitor.NewMonitorService(monitor.NewSQLRepository(db), dbtx.NewSQLTransactor(db), heartbeats, bus, channels, tags, nil, statService, nil, logger)
		}
		service := newService(statService)

		count := func(table string, monitorID string) int {
			t.Helper()
			column := "monitor_id"
			if table == "monitors" {
				column = "id"
			}
			n, err := db.NewSelect().Table(table).Where(column+" = ?", monitorID).Count(ctx)
			require.NoError(t, err)
			return n
		}
		unknown := uuid.NewString() // a tag and a channel nothing has
		dto := func(name string, channelIDs, tagIDs []string) *monitor.CreateUpdateDto {
			return &monitor.CreateUpdateDto{Type: "push", Name: name, Interval: 60, Timeout: 16, RetryInterval: 60, Active: true, Config: "{}", NotificationIds: channelIDs, TagIds: tagIDs}
		}

		// Create
		_, err := service.Create(ctx, dto("Doomed", []string{insertTxChannel(t, db)}, []string{insertTxTag(t, db), unknown}))
		require.Error(t, err)
		all, err := monitor.NewSQLRepository(db).FindAll(ctx, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, all, "a link that fails leaves no monitor behind")
		assert.Empty(t, bus.Topics(), "and no MonitorCreated")

		// Update
		channelA, tagA := insertTxChannel(t, db), insertTxTag(t, db)
		original, err := service.Create(ctx, dto("Original", []string{channelA}, []string{tagA}))
		require.NoError(t, err)
		bus.TakeTopics()
		unchanged := func(step string) {
			t.Helper()
			found, err := service.FindByID(ctx, original.ID)
			require.NoError(t, err)
			require.NotNil(t, found)
			assert.Equal(t, "Original", found.Name, step)
			assert.Equal(t, 1, count("monitor_notifications", original.ID), step)
			assert.Equal(t, 1, count("monitor_tags", original.ID), step)
			assert.Empty(t, bus.Topics(), step)
		}

		_, err = service.UpdateFull(ctx, original.ID, dto("Changed", []string{insertTxChannel(t, db)}, []string{insertTxTag(t, db), unknown}))
		require.Error(t, err)
		unchanged("a full update with a link that fails")

		name := "Changed"
		_, err = service.UpdatePartial(ctx, original.ID, &monitor.PartialUpdateDto{Name: &name, TagIds: []string{insertTxTag(t, db), unknown}}, false)
		require.Error(t, err)
		unchanged("a partial update with a link that fails")

		// Delete
		_, err = heartbeats.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: original.ID, Status: shared.MonitorStatusUp})
		require.NoError(t, err)
		bus.TakeTopics() // the heartbeat's own event
		require.Error(t, newService(failingStats{statService}).Delete(ctx, original.ID))
		unchanged("a delete whose stats fail")
		assert.Equal(t, 1, count("monitors", original.ID))
		assert.Equal(t, 1, count("heartbeats", original.ID), "the heartbeats, deleted before the stats failed, are back")

		require.NoError(t, service.Delete(ctx, original.ID))
		assert.Equal(t, []string{events.MonitorDeleted.Name()}, bus.Topics())
		assert.Zero(t, count("monitors", original.ID)+count("heartbeats", original.ID)+count("monitor_notifications", original.ID)+count("monitor_tags", original.ID))

		// A monitor that is gone is not found, rather than failing the transaction
		bus.TakeTopics() // the delete's own event
		_, err = service.UpdatePartial(ctx, original.ID, &monitor.PartialUpdateDto{Name: &name}, false)
		assert.ErrorIs(t, err, monitor.ErrMonitorNotFound)
		assert.ErrorIs(t, service.Delete(ctx, original.ID), monitor.ErrMonitorNotFound)
		assert.Empty(t, bus.Topics(), "and nothing is published")
	})
}
