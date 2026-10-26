package app

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"

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
