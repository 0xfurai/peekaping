package app

import (
	"context"
	"errors"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/queue"

	"go.uber.org/zap"
)

// provideInOrderIngester provides the ingester.Service that stores results in this process, one
// monitor's results at a time, in order: single mode's scheduler checks and push URL, and the
// cluster api's push URL and probe results on SQLite and PostgreSQL. The monitor's own status is
// still judged under the location lock, which the monitor repository holds across replicas.
func provideInOrderIngester(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	locations ingester.LocationStore,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) ingester.Service {
	return ingest.NewInOrderService(ingester.NewService(heartbeatService, certificateService, locations, eventBus, logger))
}

// LocalBus is an event bus whose events stay in the process that published them. The cluster api on
// SQLite and PostgreSQL hands the heartbeats it stored to the stats aggregation through it: on the
// cluster's bus, Redis pub/sub would give every heartbeat to every api replica, and each would
// count it.
type LocalBus struct {
	events.EventBus
}

// mirroredTopics are the topics the api's bus also delivers on its LocalBus: a heartbeat is stored
// by one replica, and the stats count it once, there
var mirroredTopics = []string{events.HeartbeatEvent.Name()}

// mirroredBus is the cluster api's event bus: the cluster's bus, which delivers an event to every
// replica that listens, and a copy of the mirrored topics to this process's own LocalBus. The
// websocket and the metrics of every replica listen on the cluster's bus for every heartbeat; the
// stats listen on the local bus for the heartbeats this replica stored.
type mirroredBus struct {
	events.EventBus // the cluster's
	local           events.EventBus
}

var _ events.EventBus = (*mirroredBus)(nil)

// PublishMessage implements events.EventBus. A mirrored topic goes to the local bus as well as the
// cluster's, and a failure of one does not keep it from the other.
func (b *mirroredBus) PublishMessage(ctx context.Context, msg events.Message) error {
	var localErr error
	for _, topic := range mirroredTopics {
		if msg.Topic == topic {
			localErr = b.local.PublishMessage(ctx, msg)
			break
		}
	}
	return errors.Join(localErr, b.EventBus.PublishMessage(ctx, msg))
}

// Close implements events.EventBus. The handlers of the events already published finish on both.
func (b *mirroredBus) Close(ctx context.Context) error {
	return errors.Join(b.EventBus.Close(ctx), b.local.Close(ctx))
}

// provideAPIEventBus provides the cluster api's event bus on SQLite and PostgreSQL: the Redis event
// bus with the notification topics sent through the queue (see provideClusterEventBus), which also
// delivers each heartbeat this replica publishes to the LocalBus. It provides that bus as the
// events.EventBus every module publishes and subscribes on, the QueuedBus the QueueConsumer hands
// the queue's events to, and the LocalBus the stats subscribe on.
func provideAPIEventBus(bus *events.Bus, q queue.Service, logger *zap.SugaredLogger) (events.EventBus, *notification_channel.QueuedBus, *LocalBus) {
	queued := notification_channel.NewQueuedBus(bus, q)
	local := events.NewMemoryBus(events.WithLogger(logger.With("component", "local_event_bus")))
	return &mirroredBus{EventBus: queued, local: local}, queued, &LocalBus{EventBus: local}
}
