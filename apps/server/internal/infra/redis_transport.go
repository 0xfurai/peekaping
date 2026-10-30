package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"peekaping/internal/modules/events"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// RedisEventChannelPrefix is the prefix of the Redis pub/sub channels events go through.
	// A topic's channel is the prefix and the topic's name. Both are part of the wire format.
	RedisEventChannelPrefix = "peekaping:events:"

	// redisPublishTimeout bounds a PUBLISH, whatever the caller's context says.
	redisPublishTimeout = 5 * time.Second
	// redisSubscribeWait is how long Subscribe waits for Redis to confirm a subscription.
	redisSubscribeWait = 2 * time.Second
	// redisHealthInterval is how long the subscription can be silent before it is pinged.
	redisHealthInterval = 15 * time.Second
	// The wait between attempts to resubscribe doubles from the minimum up to the maximum.
	redisReconnectMin = 100 * time.Millisecond
	redisReconnectMax = 5 * time.Second
)

// SerializedEvent is an event as it travels through Redis: the topic's name and the payload's
// JSON. Every version of Peekaping reads and writes this shape.
type SerializedEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// redisTransport is the events.Transport over Redis pub/sub. It publishes through the shared
// client and receives every topic through a single pub/sub connection, so the events one
// process publishes reach a subscriber in the order they were published, across topics. When
// the connection drops it resubscribes with backoff.
type redisTransport struct {
	client *redis.Client
	logger *zap.SugaredLogger

	// Reconnect and health-check timing, fields so tests can shorten them
	reconnectMin, reconnectMax time.Duration
	healthInterval             time.Duration
	subscribeWait              time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	deliver   func(events.Message)
	channels  map[string]bool          // every channel to subscribe to
	pending   map[string]chan struct{} // channels waiting for Redis to confirm, closed once they are
	started   bool
	closed    bool
	connected bool          // Redis has confirmed a subscription on the current connection
	pubsub    *redis.PubSub // the live subscription, nil between attempts
}

var _ events.Transport = (*redisTransport)(nil)

func newRedisTransport(client *redis.Client, logger *zap.SugaredLogger) *redisTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &redisTransport{
		client:         client,
		logger:         logger,
		reconnectMin:   redisReconnectMin,
		reconnectMax:   redisReconnectMax,
		healthInterval: redisHealthInterval,
		subscribeWait:  redisSubscribeWait,
		ctx:            ctx,
		cancel:         cancel,
		channels:       make(map[string]bool),
		pending:        make(map[string]chan struct{}),
	}
}

func (t *redisTransport) channel(topic string) string { return RedisEventChannelPrefix + topic }

// Start implements events.Transport.
func (t *redisTransport) Start(deliver func(events.Message)) {
	t.mu.Lock()
	t.deliver = deliver
	t.mu.Unlock()
}

// Publish implements events.Transport. It uses ctx's values and nothing of its cancellation:
// see events.Publish.
func (t *redisTransport) Publish(ctx context.Context, msg events.Message) error {
	payload, err := json.Marshal(msg.Value)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	data, err := json.Marshal(SerializedEvent{Type: msg.Topic, Payload: payload})
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisPublishTimeout)
	defer cancel()
	if err := t.client.Publish(ctx, t.channel(msg.Topic), data).Err(); err != nil {
		return fmt.Errorf("publish to redis: %w", err)
	}
	return nil
}

// Subscribe implements events.Transport. It returns once Redis has confirmed the
// subscription, or when the connection attempt fails or takes longer than subscribeWait: in
// those cases the subscription carries on in the background. While Redis is unreachable it
// returns at once, and the next successful attempt subscribes to the channel.
func (t *redisTransport) Subscribe(topic string) {
	channel := t.channel(topic)

	added, ok := t.addChannel(channel)
	if !ok {
		return
	}

	switch {
	case added.start:
		go t.run()
	case added.pubsub != nil:
		t.subscribeLive(added.pubsub, channel)
	}
	if !added.wait {
		return
	}

	select {
	case <-added.confirmed:
	case <-time.After(t.subscribeWait):
		t.logger.Warnw("Redis has not confirmed the subscription yet, carrying on", "channel", channel)
	case <-t.ctx.Done():
	}
}

// addedChannel is what Subscribe needs to know of a channel addChannel has recorded
type addedChannel struct {
	pubsub *redis.PubSub // the live subscription at that moment, nil between attempts
	start  bool          // this is the first channel, so Subscribe starts the run loop
	wait   bool          // Subscribe waits for confirmed
	// confirmed is closed once Redis has confirmed the channel, or the attempt failed. Nil if wait is false.
	confirmed chan struct{}
}

// addChannel records channel to subscribe to. It is not ok when the transport is closed or the
// channel is already recorded, and then Subscribe has nothing to do.
func (t *redisTransport) addChannel(channel string) (added addedChannel, ok bool) {
	t.mu.Lock()
	if t.closed || t.channels[channel] {
		t.mu.Unlock()
		return addedChannel{}, false
	}
	t.channels[channel] = true
	added.pubsub = t.pubsub
	added.start = !t.started
	t.started = true
	// Waiting only makes sense on the first attempt or on a connection Redis has answered on
	added.wait = added.start || t.connected
	if added.wait {
		added.confirmed = make(chan struct{})
		t.pending[channel] = added.confirmed
	}
	if added.start {
		t.wg.Add(1) // under mu, and Close sets closed under mu before it waits
	}
	t.mu.Unlock()
	return added, true
}

// subscribeLive subscribes to channel on the live connection
func (t *redisTransport) subscribeLive(pubsub *redis.PubSub, channel string) {
	// A failed write means the connection is broken, and the next attempt subscribes to it
	if err := pubsub.Subscribe(t.ctx, channel); err != nil {
		t.logger.Warnw("Failed to subscribe to a Redis channel on the live connection", "channel", channel, "error", err)
	}
}

// run keeps a subscription up until the transport is closed.
func (t *redisTransport) run() {
	defer t.wg.Done()
	failures := 0
	for t.ctx.Err() == nil {
		subscribed, err := t.subscribeOnce(failures > 0)
		if t.ctx.Err() != nil {
			return
		}
		t.releaseWaiters()
		if subscribed {
			failures = 0
		}
		wait := t.backoff(failures)
		failures++
		t.logger.Warnw("Redis event subscription failed, resubscribing",
			"error", err,
			"retry_in", wait.String(),
		)
		select {
		case <-time.After(wait):
		case <-t.ctx.Done():
			return
		}
	}
}

// backoff is the wait before attempt number failures+1: doubling from the minimum to the
// maximum, with up to half of it randomised so processes don't resubscribe in step.
func (t *redisTransport) backoff(failures int) time.Duration {
	wait := t.reconnectMin
	for range failures {
		wait *= 2
		if wait >= t.reconnectMax {
			wait = t.reconnectMax
			break
		}
	}
	return wait/2 + rand.N(wait/2+1)
}

// subscribeOnce subscribes to every channel on a new pub/sub connection and delivers
// messages until the connection fails. It reports whether Redis confirmed a subscription.
func (t *redisTransport) subscribeOnce(retry bool) (subscribed bool, err error) {
	t.mu.Lock()
	channels := make([]string, 0, len(t.channels))
	for channel := range t.channels {
		channels = append(channels, channel)
	}
	pubsub := t.client.Subscribe(t.ctx) // connects with the first channel
	t.pubsub = pubsub
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.pubsub = nil
		t.connected = false
		t.mu.Unlock()
		_ = pubsub.Close()
	}()
	// A channel added since the snapshot is subscribed to twice, which does no harm
	if err := pubsub.Subscribe(t.ctx, channels...); err != nil {
		return false, err
	}

	return t.receive(pubsub, retry)
}

// receive delivers the messages of a pub/sub connection until it fails, and reports whether Redis
// confirmed a subscription. A connection silent for a while is pinged.
func (t *redisTransport) receive(pubsub *redis.PubSub, retry bool) (subscribed bool, err error) {
	awaitingPong := false
	for {
		received, err := pubsub.ReceiveTimeout(t.ctx, t.healthInterval)
		if err != nil {
			if !t.silent(err) {
				return subscribed, err
			}
			// Silent for a while. Redis answers a ping at once, so a second silence is a dead connection.
			if awaitingPong {
				return subscribed, errors.New("no answer to a ping")
			}
			if err := pubsub.Ping(t.ctx); err != nil {
				return subscribed, err
			}
			awaitingPong = true
			continue
		}
		awaitingPong = false

		subscribed = t.onReceive(received, subscribed, retry)
	}
}

// silent reports whether err is a receive that timed out on a transport that is still open
func (t *redisTransport) silent(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout() && t.ctx.Err() == nil
}

// onReceive handles what a pub/sub connection received, and returns whether Redis has confirmed a
// subscription by now
func (t *redisTransport) onReceive(received any, subscribed, retry bool) bool {
	switch msg := received.(type) {
	case *redis.Subscription:
		if msg.Kind == "subscribe" {
			if !subscribed && retry {
				t.logger.Infow("Resubscribed to Redis events")
			}
			subscribed = true
			t.confirm(msg.Channel)
		}
	case *redis.Message:
		t.handle(msg)
	}
	return subscribed
}

// confirm marks the connection up and releases the Subscribe call waiting for channel.
func (t *redisTransport) confirm(channel string) {
	t.mu.Lock()
	t.connected = true
	if done, ok := t.pending[channel]; ok {
		close(done)
		delete(t.pending, channel)
	}
	t.mu.Unlock()
}

// releaseWaiters releases every Subscribe call still waiting: the attempt they waited for failed.
func (t *redisTransport) releaseWaiters() {
	t.mu.Lock()
	for channel, done := range t.pending {
		close(done)
		delete(t.pending, channel)
	}
	t.mu.Unlock()
}

// handle decodes the envelope of a Redis message and passes the payload on.
func (t *redisTransport) handle(msg *redis.Message) {
	topic := strings.TrimPrefix(msg.Channel, RedisEventChannelPrefix)
	var serialized SerializedEvent
	if err := json.Unmarshal([]byte(msg.Payload), &serialized); err != nil {
		t.logger.Errorw("Failed to unmarshal event", "topic", topic, "error", err)
		return
	}

	t.mu.Lock()
	deliver := t.deliver
	t.mu.Unlock()
	deliver(events.Message{Topic: topic, JSON: serialized.Payload})
}

// Close implements events.Transport. It leaves the Redis client open: the client is shared.
func (t *redisTransport) Close(ctx context.Context) error {
	t.cancel()
	t.mu.Lock()
	t.closed = true // no Subscribe can start the run loop after this, so wg.Wait below is final
	pubsub := t.pubsub
	t.mu.Unlock()
	if pubsub != nil {
		_ = pubsub.Close()
	}

	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
