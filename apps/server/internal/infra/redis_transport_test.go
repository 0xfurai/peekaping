package infra

import (
	"context"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// The topics below use the names the wire has always carried.
var (
	heartbeatTopic = events.NewTopic("heartbeat", func(h shared.HeartBeatModel) string { return h.MonitorID })
	deletedTopic   = events.NewTopic("monitor.deleted", func(id string) string { return id })
)

// redisBusConfig shortens the transport's waits, so the tests that break a connection don't take seconds.
func redisBusConfig(tr *redisTransport) {
	tr.reconnectMin, tr.reconnectMax = 5*time.Millisecond, 50*time.Millisecond
	tr.healthInterval = 200 * time.Millisecond
}

// newTestRedisBus returns a bus on its own transport over client, closed with the test.
func newTestRedisBus(t *testing.T, client *redis.Client, logger *zap.SugaredLogger) events.EventBus {
	t.Helper()
	transport := newRedisTransport(client, logger)
	redisBusConfig(transport)
	bus := events.NewBus(transport, events.WithLogger(logger))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Close(ctx)
	})
	return bus
}

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestRedisBusContract(t *testing.T) {
	eventstest.RunContract(t, func(t *testing.T) events.EventBus {
		_, client := newTestRedis(t)
		return newTestRedisBus(t, client, zap.NewNop().Sugar())
	})
}

func channelOf(topic string) string { return RedisEventChannelPrefix + topic }

func TestRedisBusSubscribeReturnsOnceRedisHasTheSubscription(t *testing.T) {
	server, client := newTestRedis(t)
	bus := newTestRedisBus(t, client, zap.NewNop().Sugar())

	events.Subscribe(events.NewSubscriber(bus, "sub"), eventstest.Alpha, func(context.Context, eventstest.Sample) {})

	assert.Equal(t, 1, server.PubSubNumSub(channelOf("eventstest.alpha"))[channelOf("eventstest.alpha")],
		"an event published right after Subscribe has somewhere to go")
}

func TestRedisBusUsesOnePubSubConnectionForEveryChannel(t *testing.T) {
	server, client := newTestRedis(t)
	bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
	sub := events.NewSubscriber(bus, "sub")

	events.Subscribe(sub, eventstest.Alpha, func(context.Context, eventstest.Sample) {})
	events.Subscribe(sub, eventstest.Beta, func(context.Context, eventstest.Sample) {})
	events.Subscribe(sub, heartbeatTopic, func(context.Context, shared.HeartBeatModel) {})
	for range 3 {
		require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1"}))
	}

	// One connection receives the three channels, one publishes
	assert.Equal(t, 2, server.CurrentConnectionCount())
}

func TestRedisBusLeavesTheSharedClientOpen(t *testing.T) {
	_, client := newTestRedis(t)
	bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
	events.Subscribe(events.NewSubscriber(bus, "sub"), eventstest.Alpha, func(context.Context, eventstest.Sample) {})

	require.NoError(t, bus.Close(context.Background()))

	assert.NoError(t, client.Ping(context.Background()).Err(), "the asynq client and the stale store use it, too")
}

func TestRedisBusResubscribesAfterTheConnectionDrops(t *testing.T) {
	server, client := newTestRedis(t)
	logger, logs := observedLogger()
	bus := newTestRedisBus(t, client, logger)
	received := make(chan eventstest.Sample, 8)
	events.Subscribe(events.NewSubscriber(bus, "sub"), eventstest.Alpha, func(_ context.Context, b eventstest.Sample) { received <- b })
	channel := channelOf("eventstest.alpha")
	require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1", Seq: 1}))
	require.Equal(t, eventstest.Sample{MonitorID: "mon-1", Seq: 1}, <-received)

	server.Close() // drops every connection
	require.Eventually(t, func() bool { return logs.FilterMessageSnippet("resubscribing").Len() > 0 }, 5*time.Second, 5*time.Millisecond,
		"the bus notices and logs that it will retry")
	require.NoError(t, server.Restart())

	require.Eventually(t, func() bool { return server.PubSubNumSub(channel)[channel] == 1 }, 5*time.Second, 5*time.Millisecond,
		"the bus resubscribes with backoff")
	require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1", Seq: 2}))
	select {
	case got := <-received:
		assert.Equal(t, eventstest.Sample{MonitorID: "mon-1", Seq: 2}, got, "an event published after the reconnect is delivered")
	case <-time.After(5 * time.Second):
		t.Fatal("no event after the reconnect")
	}
	assert.Equal(t, 1, logs.FilterMessage("Resubscribed to Redis events").Len())
}

func TestRedisBusSubscribesWhileRedisIsDown(t *testing.T) {
	server, client := newTestRedis(t)
	bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
	server.Close()
	received := make(chan eventstest.Sample, 1)

	start := time.Now()
	events.Subscribe(events.NewSubscriber(bus, "sub"), eventstest.Alpha, func(_ context.Context, b eventstest.Sample) { received <- b })

	assert.Less(t, time.Since(start), time.Second, "starting up without Redis doesn't hang")
	err := events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1"})
	assert.Error(t, err, "a publish says it didn't get through")

	require.NoError(t, server.Restart())
	channel := channelOf("eventstest.alpha")
	require.Eventually(t, func() bool { return server.PubSubNumSub(channel)[channel] == 1 }, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-2"}))
	assert.Equal(t, eventstest.Sample{MonitorID: "mon-2"}, <-received)
}

func TestRedisBusSubscribesToManyTopicsQuicklyWhileRedisIsDown(t *testing.T) {
	server, client := newTestRedis(t)
	server.Close()
	transport := newRedisTransport(client, zap.NewNop().Sugar())
	// Production waits: a retry is seconds away, so nothing frees a waiting Subscribe early
	transport.reconnectMin, transport.reconnectMax = 3*time.Second, 3*time.Second
	bus := events.NewBus(transport)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Close(ctx)
	})
	topics := []events.Topic[eventstest.Sample]{eventstest.Alpha, eventstest.Beta}
	for i := range 4 {
		topics = append(topics, events.NewTopic(fmt.Sprintf("eventstest.extra-%d", i), func(s eventstest.Sample) string { return s.MonitorID }))
	}

	start := time.Now()
	sub := events.NewSubscriber(bus, "sub")
	for _, topic := range topics {
		events.Subscribe(sub, topic, func(context.Context, eventstest.Sample) {})
	}

	assert.Less(t, time.Since(start), time.Second, "a process starting without Redis subscribes to all its topics without waiting on each")
}

// heartbeatEnvelope is an event as a version before heartbeats carried a probe published it
const heartbeatEnvelope = `{"type":"heartbeat","payload":{"id":"hb-1","monitor_id":"mon-1","status":0,"msg":"connection refused","ping":42,"duration":7,"down_count":2,"retries":1,"important":true,"time":"2026-10-17T09:30:00Z","end_time":"2026-10-17T09:30:01Z","notified":true}}`

// probeHeartbeatEnvelope is the same event as a version that records the probe publishes it
const probeHeartbeatEnvelope = `{"type":"heartbeat","payload":{"id":"hb-1","monitor_id":"mon-1","probe_id":"local","status":0,"msg":"connection refused","ping":42,"duration":7,"down_count":2,"retries":1,"important":true,"time":"2026-10-17T09:30:00Z","end_time":"2026-10-17T09:30:01Z","notified":true}}`

func heartbeat() shared.HeartBeatModel {
	return shared.HeartBeatModel{
		ID:        "hb-1",
		MonitorID: "mon-1",
		Status:    shared.MonitorStatusDown,
		Msg:       "connection refused",
		Ping:      42,
		Duration:  7,
		DownCount: 2,
		Retries:   1,
		Important: true,
		Time:      time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 10, 17, 9, 30, 1, 0, time.UTC),
		Notified:  true,
	}
}

func TestRedisTransportSubscribeDuringCloseIsSafe(t *testing.T) {
	// Run with -race: the first Subscribe starts the run loop while Close waits for it
	_, client := newTestRedis(t)
	for i := range 50 {
		transport := newRedisTransport(client, zap.NewNop().Sugar())
		transport.Start(func(events.Message) {})
		done := make(chan struct{})
		go func() {
			defer close(done)
			transport.Subscribe(fmt.Sprintf("eventstest.race-%d", i))
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		require.NoError(t, transport.Close(ctx))
		cancel()
		<-done
	}
}

func TestRedisBusWireFormat(t *testing.T) {
	t.Run("an event published by an older version reaches a typed handler", func(t *testing.T) {
		server, client := newTestRedis(t)
		bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
		received := make(chan shared.HeartBeatModel, 1)
		events.Subscribe(events.NewSubscriber(bus, "sub"), heartbeatTopic, func(_ context.Context, hb shared.HeartBeatModel) { received <- hb })

		require.Equal(t, 1, server.Publish("peekaping:events:heartbeat", heartbeatEnvelope))

		assert.Equal(t, heartbeat(), <-received)
	})

	t.Run("fields a newer version added are ignored", func(t *testing.T) {
		server, client := newTestRedis(t)
		bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
		received := make(chan shared.HeartBeatModel, 1)
		events.Subscribe(events.NewSubscriber(bus, "sub"), heartbeatTopic, func(_ context.Context, hb shared.HeartBeatModel) { received <- hb })

		server.Publish("peekaping:events:heartbeat", `{"type":"heartbeat","version":2,"payload":{"id":"hb-1","monitor_id":"mon-1","new_field":[1,2]}}`)

		assert.Equal(t, shared.HeartBeatModel{ID: "hb-1", MonitorID: "mon-1"}, <-received)
	})

	t.Run("a monitor ID travels as a bare string", func(t *testing.T) {
		server, client := newTestRedis(t)
		bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
		received := make(chan string, 1)
		events.Subscribe(events.NewSubscriber(bus, "sub"), deletedTopic, func(_ context.Context, id string) { received <- id })

		server.Publish("peekaping:events:monitor.deleted", `{"type":"monitor.deleted","payload":"mon-1"}`)

		assert.Equal(t, "mon-1", <-received)
	})

	t.Run("what the bus publishes is the envelope older versions read", func(t *testing.T) {
		server, client := newTestRedis(t)
		bus := newTestRedisBus(t, client, zap.NewNop().Sugar())
		raw := client.Subscribe(context.Background(), "peekaping:events:heartbeat", "peekaping:events:monitor.deleted")
		defer raw.Close()
		_, err := raw.Receive(context.Background())
		require.NoError(t, err)
		_, err = raw.Receive(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, server.PubSubNumSub("peekaping:events:heartbeat")["peekaping:events:heartbeat"])

		fromLocal := heartbeat()
		fromLocal.ProbeID = "local"
		require.NoError(t, events.Publish(context.Background(), bus, heartbeatTopic, fromLocal))
		require.NoError(t, events.Publish(context.Background(), bus, deletedTopic, "mon-1"))

		for _, want := range []struct{ channel, envelope string }{
			{"peekaping:events:heartbeat", probeHeartbeatEnvelope},
			{"peekaping:events:monitor.deleted", `{"type":"monitor.deleted","payload":"mon-1"}`},
		} {
			msg, err := raw.ReceiveMessage(context.Background())
			require.NoError(t, err)
			assert.Equal(t, want.channel, msg.Channel)
			assert.JSONEq(t, want.envelope, msg.Payload)
		}
	})

	t.Run("an envelope that isn't JSON is logged with its topic and the next event still arrives", func(t *testing.T) {
		server, client := newTestRedis(t)
		logger, logs := observedLogger()
		bus := newTestRedisBus(t, client, logger)
		received := make(chan shared.HeartBeatModel, 1)
		events.Subscribe(events.NewSubscriber(bus, "sub"), heartbeatTopic, func(_ context.Context, hb shared.HeartBeatModel) { received <- hb })

		server.Publish("peekaping:events:heartbeat", "not json")
		server.Publish("peekaping:events:heartbeat", `{"type":"heartbeat","payload":{"id":"hb-1","monitor_id":"mon-1","status":"up"}}`)
		server.Publish("peekaping:events:heartbeat", heartbeatEnvelope)

		assert.Equal(t, heartbeat(), <-received)
		require.Equal(t, 1, logs.FilterMessage("Failed to unmarshal event").Len())
		assert.Equal(t, "heartbeat", logs.FilterMessage("Failed to unmarshal event").All()[0].ContextMap()["topic"])
		require.Equal(t, 1, logs.FilterMessageSnippet("Failed to decode event").Len(), "a payload of the wrong shape is dropped")
		assert.Equal(t, "heartbeat", logs.FilterMessageSnippet("Failed to decode event").All()[0].ContextMap()["topic"])
	})
}

func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core).Sugar(), logs
}
