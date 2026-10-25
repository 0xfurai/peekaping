package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// received counts the heartbeats a bus delivers to a handler
func received(bus events.EventBus) *int {
	n := new(int)
	events.Subscribe(events.NewSubscriber(bus, "test"), events.HeartbeatEvent, func(context.Context, shared.HeartBeatModel) { *n++ })
	return n
}

func TestMirroredBus_DeliversAHeartbeatToTheClusterAndToTheLocalBusOnce(t *testing.T) {
	cluster, local := eventstest.NewBus(), eventstest.NewBus()
	bus := &mirroredBus{EventBus: cluster, local: local}
	onCluster, onLocal := received(bus), received(local)

	require.NoError(t, events.Publish(context.Background(), bus, events.HeartbeatEvent, shared.HeartBeatModel{MonitorID: "monitor-1"}))

	assert.Equal(t, 1, *onCluster, "the websocket and the metrics of every replica hear of it on the cluster's bus")
	assert.Equal(t, 1, *onLocal, "and the stats of this replica, on its own")
}

// What the other replicas publish reaches this one over the cluster's bus only, so its stats never
// count it
func TestMirroredBus_AHeartbeatTheClusterDeliversIsNotCountedLocally(t *testing.T) {
	cluster, local := eventstest.NewBus(), eventstest.NewBus()
	bus := &mirroredBus{EventBus: cluster, local: local}
	onCluster, onLocal := received(bus), received(local)

	require.NoError(t, cluster.PublishJSON(events.HeartbeatEvent.Name(), `{"monitor_id":"monitor-1"}`))

	assert.Equal(t, 1, *onCluster)
	assert.Zero(t, *onLocal)
}

func TestMirroredBus_OtherTopicsStayOnTheClusterBus(t *testing.T) {
	cluster, local := eventstest.NewBus(), eventstest.NewBus()
	bus := &mirroredBus{EventBus: cluster, local: local}
	var seen int
	events.Subscribe(events.NewSubscriber(local, "test"), events.MonitorDeleted, func(context.Context, string) { seen++ })

	require.NoError(t, events.Publish(context.Background(), bus, events.MonitorDeleted, "monitor-1"))

	assert.Zero(t, seen, "only the heartbeats are mirrored")
	assert.Len(t, eventstest.Published(cluster, events.MonitorDeleted), 1)
}

func TestMirroredBus_AFailureOfOneBusDoesNotKeepTheOtherFromTheEvent(t *testing.T) {
	cluster, local := eventstest.NewBus(), eventstest.NewBus()
	bus := &mirroredBus{EventBus: cluster, local: local}
	onLocal := received(local)
	require.NoError(t, cluster.Close(context.Background()))

	err := events.Publish(context.Background(), bus, events.HeartbeatEvent, shared.HeartBeatModel{MonitorID: "monitor-1"})

	assert.True(t, errors.Is(err, events.ErrClosed), "the failure is reported")
	assert.Equal(t, 1, *onLocal, "and the local bus has the heartbeat all the same, so the stats count it")
}

func TestMirroredBus_CloseClosesBothBuses(t *testing.T) {
	cluster, local := eventstest.NewBus(), eventstest.NewBus()
	bus := &mirroredBus{EventBus: cluster, local: local}

	require.NoError(t, bus.Close(context.Background()))

	assert.ErrorIs(t, events.Publish(context.Background(), cluster, events.HeartbeatEvent, shared.HeartBeatModel{}), events.ErrClosed)
	assert.ErrorIs(t, events.Publish(context.Background(), local, events.HeartbeatEvent, shared.HeartBeatModel{}), events.ErrClosed)
}

// On SQLite and PostgreSQL the api stores what it takes and counts the stats of it from its own
// local bus
func TestRegisterClusterAPI_HasALocalBusForTheStatsOnSQLStorage(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(_ ingester.Service, local *LocalBus, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		assert.NotNil(t, local)
		assert.IsType(t, &mirroredBus{}, bus)
	}))
}

// MongoDB has no probes: its api enqueues what it takes for the ingester, which stores it and
// counts the stats of it, so the api has no local bus and does not store
func TestRegisterClusterAPI_EnqueuesForTheIngesterOnMongoStorage(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.DBType = "mongo"
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, cfg))
	assert.Error(t, container.Invoke(func(*LocalBus) {}), "no local bus: the ingester counts the stats")

	require.NoError(t, container.Invoke(func(service ingester.Service, bus events.EventBus, client *asynq.Client, inspector *asynq.Inspector) {
		closeEventBus(t, bus)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
		_, mirrored := bus.(*mirroredBus)
		assert.False(t, mirrored)

		// Ingesting needs no database: it queues the result
		require.NoError(t, service.Ingest(context.Background(), &ingester.Result{
			MonitorID: "monitor-1", StartTime: time.Now(), EndTime: time.Now(), Status: shared.MonitorStatusUp,
		}))
		queued, err := inspector.ListPendingTasks("ingester")
		require.NoError(t, err)
		assert.Len(t, queued, 1, "the result waits for the ingester")
	}))
}
