package notification_channel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/stale"
)

const (
	// QueueName is the asynq queue that carries the events the notification listener sends from
	QueueName = "notification"
	// TaskTypeNotification is the type of a task that carries one such event
	TaskTypeNotification = "notification:event"

	// taskMaxRetry is how often a task is tried again after the api failed to take it, which
	// only happens while the replica that picked it up is closing its event bus. asynq waits
	// longer before each retry, from about 15 seconds to several minutes, so five tries outlast
	// a rolling restart of the api, and any replica can take the task on the next one.
	taskMaxRetry = 5
	// taskTimeout bounds one try. Taking a task only hands its event to the api's event bus,
	// which doesn't wait for the listener, so this is generous; it just frees a stuck task.
	taskTimeout = 30 * time.Second
)

// queuedTopics are the topics the notification listener subscribes to, which QueuedBus sends
// through the queue instead of Redis pub/sub. Redis hands a published event to every api
// replica, so each would send the notification, while a queue gives a task to one replica. The
// listener is the only subscriber of these topics: a topic that another part of the api needs
// on every replica, such as the heartbeat the websocket pushes, stays on pub/sub.
var queuedTopics = []string{
	events.ImportantHeartbeat.Name(),
	certificate.CertificateExpiry.Name(),
	stale.MonitorStale.Name(),
	stale.MonitorStaleRecovered.Name(),
	probe.ProbeOffline.Name(),
	probe.ProbeOnline.Name(),
}

// queuedEvent is the payload of a notification task: the topic's name, and the event's payload
// as JSON, the way it travels over Redis pub/sub.
type queuedEvent struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// QueuedBus is the event bus of the cluster api: the Redis event bus, except that publishing one
// of the notification topics enqueues a task on the notification queue. The stored heartbeats, the
// watchdog and the probe sweep publish those topics as they would on any bus, and the api's
// QueueConsumer takes each task on one replica and hands it to that replica's own bus, so the
// notification listener sends each notification once however many replicas run.
//
// The bus still subscribes to the Redis channels of the notification topics when a listener asks
// for them. Nothing publishes there now, but an ingester or producer of an earlier version does,
// and during a rolling upgrade each api replica sends such a notification, as they all did
// before, instead of losing it.
type QueuedBus struct {
	*events.Bus
	queue  queue.Service
	closed atomic.Bool
}

var _ events.EventBus = (*QueuedBus)(nil)

// NewQueuedBus returns the Redis event bus bus with the notification topics routed to q.
func NewQueuedBus(bus *events.Bus, q queue.Service) *QueuedBus {
	return &QueuedBus{Bus: bus, queue: q}
}

// PublishMessage implements events.EventBus. A notification topic is enqueued once, for the
// api replica that takes it first: it is not published on Redis. Every other topic is.
func (b *QueuedBus) PublishMessage(ctx context.Context, msg events.Message) error {
	if !slices.Contains(queuedTopics, msg.Topic) {
		return b.Bus.PublishMessage(ctx, msg)
	}
	if b.closed.Load() {
		return events.ErrClosed
	}

	payload := msg.JSON
	if payload == nil {
		var err error
		if payload, err = json.Marshal(msg.Value); err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
	}
	opts := &queue.EnqueueOptions{
		Queue:    QueueName,
		MaxRetry: taskMaxRetry,
		Timeout:  taskTimeout,
		// asynq keeps no finished task: it is of no use, and its payload holds a heartbeat
	}
	// An event announces a change that is already stored, so it is queued even if the request
	// behind it is cancelled: see events.Publish
	err := b.queue.Enqueue(context.WithoutCancel(ctx), TaskTypeNotification, queuedEvent{Topic: msg.Topic, Payload: payload}, opts)
	if err != nil {
		return fmt.Errorf("enqueue on the %s queue: %w", QueueName, err)
	}
	return nil
}

// Close implements events.EventBus. Publishing a notification topic returns events.ErrClosed
// from here on, as every other topic does.
func (b *QueuedBus) Close(ctx context.Context) error {
	b.closed.Store(true)
	return b.Bus.Close(ctx)
}
