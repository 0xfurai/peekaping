package infra

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/events"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A subscription the live connection refuses is logged, and the next attempt makes it
func TestRedisTransportWarnsWhenTheLiveConnectionRefusesASubscription(t *testing.T) {
	_, client := newTestRedis(t)
	logger, logs := observedLogger()
	transport := newRedisTransport(client, logger)
	t.Cleanup(transport.cancel)
	transport.started = true // no run loop: the connection below is the one Subscribe finds
	pubsub := client.Subscribe(transport.ctx)
	require.NoError(t, pubsub.Close())
	transport.pubsub = pubsub

	transport.Subscribe("eventstest.alpha")

	warnings := logs.FilterMessage("Failed to subscribe to a Redis channel on the live connection").All()
	require.Len(t, warnings, 1)
	assert.Equal(t, channelOf("eventstest.alpha"), warnings[0].ContextMap()["channel"])
	assert.NotEmpty(t, warnings[0].ContextMap()["error"])
	assert.True(t, transport.channels[channelOf("eventstest.alpha")], "the channel is kept for the next attempt")
}

// Subscribe on a connection Redis has answered on waits for the confirmation, but not for ever
func TestRedisTransportSubscribeCarriesOnWhenRedisDoesNotConfirm(t *testing.T) {
	_, client := newTestRedis(t)
	logger, logs := observedLogger()
	transport := newRedisTransport(client, logger)
	t.Cleanup(transport.cancel)
	transport.started, transport.connected = true, true
	transport.subscribeWait = 30 * time.Millisecond

	start := time.Now()
	transport.Subscribe("eventstest.alpha")

	assert.GreaterOrEqual(t, time.Since(start), 30*time.Millisecond, "it waited for the confirmation")
	warnings := logs.FilterMessage("Redis has not confirmed the subscription yet, carrying on").All()
	require.Len(t, warnings, 1)
	assert.Equal(t, channelOf("eventstest.alpha"), warnings[0].ContextMap()["channel"])
}

// Closing the transport frees a Subscribe that waits for a confirmation
func TestRedisTransportCloseFreesASubscribeThatWaits(t *testing.T) {
	_, client := newTestRedis(t)
	logger, logs := observedLogger()
	transport := newRedisTransport(client, logger)
	transport.started, transport.connected = true, true
	transport.subscribeWait = time.Minute
	done := make(chan struct{})
	go func() {
		defer close(done)
		transport.Subscribe("eventstest.alpha")
	}()

	time.Sleep(50 * time.Millisecond)
	transport.cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe still waits after the transport was closed")
	}
	assert.Zero(t, logs.Len(), "nothing went wrong")
}

// silentRedis is a server that confirms a subscription and then says nothing, not even to a ping.
// It returns its address.
func silentRedis(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSilentRedis(conn)
		}
	}()
	return listener.Addr().String()
}

// serveSilentRedis reads commands (RESP arrays of bulk strings) and answers HELLO with an error, as
// a Redis that predates it does, and SUBSCRIBE with a confirmation of each channel. It ignores the
// rest.
func serveSilentRedis(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
		if err != nil {
			return
		}
		args := make([]string, count)
		for i := range args {
			size, err := reader.ReadString('\n') // $<len>
			if err != nil {
				return
			}
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(size, "$")))
			data := make([]byte, n+2) // the argument and its \r\n
			if _, err := io.ReadFull(reader, data); err != nil {
				return
			}
			args[i] = string(data[:n])
		}
		switch strings.ToLower(args[0]) {
		case "hello":
			_, _ = fmt.Fprint(conn, "-ERR unknown command 'HELLO'\r\n")
		case "subscribe":
			for i, channel := range args[1:] {
				_, _ = fmt.Fprintf(conn, "*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:%d\r\n", len(channel), channel, i+1)
			}
		}
	}
}

// Redis answers a ping at once, so a connection that is silent through two health intervals is dead
func TestRedisTransportResubscribesWhenAPingGetsNoAnswer(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: silentRedis(t), Protocol: 2, DisableIdentity: true})
	t.Cleanup(func() { _ = client.Close() })
	logger, logs := observedLogger()
	transport := newRedisTransport(client, logger)
	redisBusConfig(transport)
	transport.Start(func(events.Message) {})
	t.Cleanup(func() { _ = transport.Close(t.Context()) })

	transport.Subscribe("eventstest.alpha")

	require.Eventually(t, func() bool { return logs.FilterMessage("Redis event subscription failed, resubscribing").Len() > 0 },
		5*time.Second, 10*time.Millisecond, "the dead connection is noticed")
	failure := logs.FilterMessage("Redis event subscription failed, resubscribing").All()[0]
	assert.Equal(t, "no answer to a ping", fmt.Sprint(failure.ContextMap()["error"]))
}
