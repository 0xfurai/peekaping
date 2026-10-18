package websocket_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/websocket"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

const (
	monitorA = "monitor-a"
	monitorB = "monitor-b"
	wait     = 3 * time.Second
)

// fakeBus delivers published events to its subscribers synchronously.
type fakeBus struct {
	mu       sync.Mutex
	handlers map[events.EventType][]events.EventHandler
}

func (b *fakeBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handlers == nil {
		b.handlers = map[events.EventType][]events.EventHandler{}
	}
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *fakeBus) Publish(event events.Event) {
	b.mu.Lock()
	handlers := append([]events.EventHandler(nil), b.handlers[event.Type]...)
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(event)
	}
}

func (b *fakeBus) Close() error { return nil }

func (b *fakeBus) heartbeat(monitorID, msg string) {
	b.Publish(events.Event{Type: events.HeartbeatEvent, Payload: &heartbeat.Model{MonitorID: monitorID, Msg: msg}})
}

type harness struct {
	url    string
	bus    *fakeBus
	tokens *auth.TokenMaker
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "websocket.db"))
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)

	logger := zap.NewNop().Sugar()
	settings := setting.NewService(setting.NewSQLRepository(db), logger)
	require.NoError(t, settings.InitializeSettings(ctx))
	tokens := auth.NewTokenMaker(settings, logger)
	bus := &fakeBus{}
	server, err := websocket.NewServer(&config.Config{}, bus, tokens, logger)
	require.NoError(t, err)
	srv := httptest.NewServer(server)
	// Dropping the connections ends the open long polls, so Close doesn't wait
	// for them. server.Close isn't called: engine.io then writes to those
	// polls from another goroutine, a data race inside the library.
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &harness{srv.URL, bus, tokens}
}

func (h *harness) accessToken(t *testing.T) string {
	t.Helper()
	token, err := h.tokens.CreateAccessToken(context.Background(), &auth.Model{ID: "user-1", Email: "admin@example.com"})
	require.NoError(t, err)
	return token
}

// client speaks Engine.IO v4 long polling, the transport a socket.io client
// starts with.
type client struct {
	url     string
	packets chan string
}

type event struct {
	name string
	hb   heartbeat.Model
}

// dial opens an Engine.IO session and sends the socket.io CONNECT packet. A
// reader keeps one long poll open and answers pings.
func (h *harness) dial(t *testing.T, token string) *client {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"polling"}}
	if token != "" {
		query.Set("token", token)
	}
	base := h.url + "/socket.io/?" + query.Encode()
	resp, err := http.Get(base)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.True(t, strings.HasPrefix(string(body), "0"), "handshake: %s", body)
	var open struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal(body[1:], &open))

	c := &client{url: base + "&sid=" + url.QueryEscape(open.SID), packets: make(chan string, 256)}
	require.NoError(t, c.post("40"))
	go c.read()
	return c
}

func (c *client) post(packet string) error {
	resp, err := http.Post(c.url, "text/plain;charset=UTF-8", strings.NewReader(packet))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %q: %s", packet, resp.Status)
	}
	return nil
}

func (c *client) read() {
	defer close(c.packets)
	for {
		resp, err := http.Get(c.url)
		if err != nil {
			return
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			return
		}
		for _, packet := range strings.Split(string(body), "\x1e") {
			switch packet {
			case "1": // close
				return
			case "2": // ping
				_ = c.post("3")
			case "6": // noop
			default:
				c.packets <- packet
			}
		}
	}
}

// next returns the next socket.io packet, or fails after a few seconds.
func (c *client) next(t *testing.T) string {
	t.Helper()
	select {
	case packet, ok := <-c.packets:
		require.True(t, ok, "session closed")
		return packet
	case <-time.After(wait):
		require.FailNow(t, "no packet within "+wait.String())
		return ""
	}
}

func (h *harness) connect(t *testing.T) *client {
	t.Helper()
	c := h.dial(t, h.accessToken(t))
	packet := c.next(t)
	require.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
	return c
}

func (c *client) emit(t *testing.T, name, arg string) {
	t.Helper()
	packet, err := json.Marshal([]string{name, arg})
	require.NoError(t, err)
	require.NoError(t, c.post("42"+string(packet)))
}

// until reads events until one carries msg and returns every event read,
// that one included.
func (c *client) until(t *testing.T, msg string) []event {
	t.Helper()
	var seen []event
	deadline := time.After(wait)
	for {
		var packet string
		select {
		case p, ok := <-c.packets:
			require.True(t, ok, "session closed")
			packet = p
		case <-deadline:
			require.FailNow(t, "no heartbeat "+msg+" within "+wait.String())
		}
		if !strings.HasPrefix(packet, "42") {
			continue
		}
		var args []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(packet[2:]), &args))
		require.Len(t, args, 2, packet)
		var e event
		require.NoError(t, json.Unmarshal(args[0], &e.name))
		require.NoError(t, json.Unmarshal(args[1], &e.hb))
		seen = append(seen, e)
		if e.hb.Msg == msg {
			return seen
		}
	}
}

// join enters a room and waits until it delivers.
func (c *client) join(t *testing.T, bus *fakeBus, room, monitorID string) {
	t.Helper()
	c.emit(t, "join_room", room)
	c.waitUntilJoined(t, bus, room, monitorID)
}

// waitUntilJoined publishes test heartbeats for monitorID until one arrives.
// Socket.io runs each event handler in its own goroutine, so the room isn't
// joined when the POST returns.
func (c *client) waitUntilJoined(t *testing.T, bus *fakeBus, room, monitorID string) {
	t.Helper()
	msg := "joined " + room
	deadline := time.Now().Add(wait)
	for {
		bus.heartbeat(monitorID, msg)
		select {
		case packet, ok := <-c.packets:
			require.True(t, ok, "session closed")
			if strings.Contains(packet, msg) {
				return
			}
		case <-time.After(20 * time.Millisecond):
		}
		require.True(t, time.Now().Before(deadline), "%s delivered nothing", room)
	}
}

func messages(seen []event) []string {
	var out []string
	for _, e := range seen {
		out = append(out, e.hb.Msg)
	}
	return out
}

func TestConnectionNeedsAValidAccessToken(t *testing.T) {
	h := newHarness(t)
	refresh, err := h.tokens.CreateRefreshToken(context.Background(), &auth.Model{ID: "user-1", Email: "admin@example.com"})
	require.NoError(t, err)

	for name, token := range map[string]string{"no token": "", "not a JWT": "not-a-jwt", "refresh token": refresh} {
		t.Run(name, func(t *testing.T) {
			packet := h.dial(t, token).next(t)
			assert.True(t, strings.HasPrefix(packet, "44"), "expected CONNECT_ERROR, got %s", packet)
		})
	}

	t.Run("access token", func(t *testing.T) {
		packet := h.dial(t, h.accessToken(t)).next(t)
		assert.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
	})
}

func TestMonitorRoomGetsOnlyThatMonitorsHeartbeats(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)

	h.bus.heartbeat(monitorB, "b-1")
	h.bus.heartbeat(monitorA, "a-1")

	seen := c.until(t, "a-1")
	assert.NotContains(t, messages(seen), "b-1")
	last := seen[len(seen)-1]
	assert.Equal(t, "monitor:"+monitorA+":heartbeat", last.name)
	assert.Equal(t, monitorA, last.hb.MonitorID)
}

func TestAllRoomGetsEveryHeartbeat(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:all", monitorA)

	h.bus.heartbeat(monitorB, "b-1")
	h.bus.heartbeat(monitorA, "a-1")

	seen := c.until(t, "a-1")
	byMsg := map[string]event{}
	for _, e := range seen {
		byMsg[e.hb.Msg] = e
	}
	require.Contains(t, byMsg, "b-1")
	assert.Equal(t, "monitor:all:heartbeat", byMsg["b-1"].name)
	assert.Equal(t, monitorB, byMsg["b-1"].hb.MonitorID)
	assert.Equal(t, "monitor:all:heartbeat", byMsg["a-1"].name)
	assert.Equal(t, monitorA, byMsg["a-1"].hb.MonitorID)
}

func TestLeaveRoomStopsDelivery(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
	c.join(t, h.bus, "monitor:"+monitorB, monitorB)

	c.emit(t, "leave_room", "monitor:"+monitorA)

	// The leave runs in its own goroutine too, so publish rounds until A's
	// heartbeat stops arriving. B's heartbeat, still subscribed, closes each
	// round.
	deliversA := func(round int) bool {
		a, b := fmt.Sprintf("a-%d", round), fmt.Sprintf("b-%d", round)
		h.bus.heartbeat(monitorA, a)
		h.bus.heartbeat(monitorB, b)
		return slices.Contains(messages(c.until(t, b)), a)
	}
	deadline := time.Now().Add(wait)
	round := 0
	for deliversA(round) {
		require.True(t, time.Now().Before(deadline), "monitor:%s still delivers after leave_room", monitorA)
		round++
	}
	assert.False(t, deliversA(round+1), "monitor:%s delivered again after leave_room", monitorA)
}
