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
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
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

// publishHeartbeat publishes a heartbeat the way the ingester does.
func publishHeartbeat(t *testing.T, bus *eventstest.Bus, monitorID, msg string) {
	t.Helper()
	assert.NoError(t, events.Publish(context.Background(), bus, events.HeartbeatEvent, heartbeat.Model{MonitorID: monitorID, Msg: msg}))
}

type harness struct {
	url    string
	bus    *eventstest.Bus
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
	bus := eventstest.NewBus()
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

// dial opens an Engine.IO session and sends the socket.io CONNECT packet with
// token in its auth payload, as a socket.io client does (no payload when token is
// empty).
func (h *harness) dial(t *testing.T, token string) *client {
	t.Helper()
	connect := "40"
	if token != "" {
		payload, err := json.Marshal(map[string]string{"token": token})
		require.NoError(t, err)
		connect += string(payload)
	}
	return h.open(t, nil, connect)
}

// dialWithQueryToken sends token in the query string, where the web app used to
// put it, and no auth payload.
func (h *harness) dialWithQueryToken(t *testing.T, token string) *client {
	t.Helper()
	return h.open(t, url.Values{"token": {token}}, "40")
}

// open opens an Engine.IO session with the extra query parameters and sends the
// socket.io CONNECT packet. A reader keeps one long poll open and answers pings.
func (h *harness) open(t *testing.T, extra url.Values, connect string) *client {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"polling"}}
	for name, values := range extra {
		query[name] = values
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
	require.NoError(t, c.post(connect))
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
func (c *client) join(t *testing.T, bus *eventstest.Bus, room, monitorID string) {
	t.Helper()
	c.emit(t, "join_room", room)
	c.waitUntilJoined(t, bus, room, monitorID)
}

// waitUntilJoined publishes test heartbeats for monitorID until one arrives.
// Socket.io runs each event handler in its own goroutine, so the room isn't
// joined when the POST returns.
func (c *client) waitUntilJoined(t *testing.T, bus *eventstest.Bus, room, monitorID string) {
	t.Helper()
	msg := "joined " + room
	deadline := time.Now().Add(wait)
	for {
		publishHeartbeat(t, bus, monitorID, msg)
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

// The token is read from the socket.io auth payload only. One in the query string
// is ignored, so it stops showing up in the access logs of a proxy.
func TestConnectionTakesTheTokenFromTheAuthPayloadOnly(t *testing.T) {
	h := newHarness(t)
	token := h.accessToken(t)

	t.Run("auth payload", func(t *testing.T) {
		packet := h.dial(t, token).next(t)
		assert.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
	})
	t.Run("query string", func(t *testing.T) {
		packet := h.dialWithQueryToken(t, token).next(t)
		assert.True(t, strings.HasPrefix(packet, "44"), "expected CONNECT_ERROR, got %s", packet)
	})
	t.Run("query string and an empty auth payload", func(t *testing.T) {
		packet := h.open(t, url.Values{"token": {token}}, "40{}").next(t)
		assert.True(t, strings.HasPrefix(packet, "44"), "expected CONNECT_ERROR, got %s", packet)
	})
	t.Run("token that isn't a string", func(t *testing.T) {
		packet := h.open(t, nil, `40{"token":5}`).next(t)
		assert.True(t, strings.HasPrefix(packet, "44"), "expected CONNECT_ERROR, got %s", packet)
	})
}

func TestMonitorRoomGetsOnlyThatMonitorsHeartbeats(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)

	publishHeartbeat(t, h.bus, monitorB, "b-1")
	publishHeartbeat(t, h.bus, monitorA, "a-1")

	seen := c.until(t, "a-1")
	assert.NotContains(t, messages(seen), "b-1")
	last := seen[len(seen)-1]
	assert.Equal(t, "monitor:"+monitorA+":heartbeat", last.name)
	assert.Equal(t, monitorA, last.hb.MonitorID)
}

// The event carries the probe that ran the check, under the name the heartbeat API uses.
func TestHeartbeatEventCarriesTheProbe(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)

	assert.NoError(t, events.Publish(context.Background(), h.bus, events.HeartbeatEvent, heartbeat.Model{MonitorID: monitorA, ProbeID: "probe-frankfurt", Msg: "from frankfurt"}))

	deadline := time.After(wait)
	for {
		select {
		case packet, ok := <-c.packets:
			require.True(t, ok, "session closed")
			if strings.Contains(packet, "from frankfurt") {
				assert.True(t, strings.HasPrefix(packet, `42["monitor:`+monitorA+`:heartbeat",`), packet)
				assert.Contains(t, packet, `"probe_id":"probe-frankfurt"`)
				return
			}
		case <-deadline:
			require.FailNow(t, "no heartbeat within "+wait.String())
		}
	}
}

func TestAllRoomGetsEveryHeartbeat(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:all", monitorA)

	publishHeartbeat(t, h.bus, monitorB, "b-1")
	publishHeartbeat(t, h.bus, monitorA, "a-1")

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
		publishHeartbeat(t, h.bus, monitorA, a)
		publishHeartbeat(t, h.bus, monitorB, b)
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

// The room name is client input. A message without a string room name is
// dropped, and the connection keeps working.
func TestMalformedRoomMessagesDontCrashTheServer(t *testing.T) {
	h := newHarness(t)

	for _, event := range []string{"join_room", "leave_room"} {
		for _, arg := range []struct{ name, json string }{
			{"no argument", ""},
			{"a number", ",1"},
			{"an object", ",{}"},
		} {
			t.Run(event+" with "+arg.name, func(t *testing.T) {
				c := h.connect(t)
				require.NoError(t, c.post(`42["`+event+`"`+arg.json+`]`))

				c.join(t, h.bus, "monitor:"+monitorA, monitorA)
			})
		}
	}
}

// A logged-in client can post the bare packet "42", an EVENT with no payload. Older
// socket.io parsers let it through with nil data, and socket.io then cast the data
// unchecked in its own goroutine, so no handler recover could catch the panic. The
// library now closes that client's session, and the server has to keep serving others.
func TestEventPacketWithoutPayloadDoesntCrashTheServer(t *testing.T) {
	h := newHarness(t)
	bad := h.connect(t)
	// The library closes the session and may abort this request with a 429, but the
	// packet has to reach the server for the test to mean anything.
	if err := bad.post("42"); err != nil {
		require.EqualError(t, err, `POST "42": 429 Too Many Requests`)
	}

	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
}
