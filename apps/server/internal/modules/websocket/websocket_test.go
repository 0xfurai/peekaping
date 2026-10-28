package websocket_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

const (
	// monitorA is a monitor of workspace A, monitorB one of workspace B
	monitorA = "5b0c6f1a-2d3e-4f40-8a51-0c7d9e1b2a01"
	monitorB = "5b0c6f1a-2d3e-4f40-8a51-0c7d9e1b2a02"
	// monitorA2 is a second monitor of workspace A
	monitorA2 = "5b0c6f1a-2d3e-4f40-8a51-0c7d9e1b2a03"

	workspaceA = workspacetest.ID
	workspaceB = workspacetest.Other
)

// roomOf is the room of a workspace
func roomOf(id workspace.ID) string { return "workspace:" + string(id) }

// heartbeatOf is the event a room delivers a heartbeat as
func heartbeatOf(room string) string { return room + ":heartbeat" }

// wait is how long a test waits for a packet, a join or a delivery. It only bounds a failure: a
// test goes on as soon as the state is there, so the bound can fit a CI runner that the other
// packages' tests starve.
const wait = 30 * time.Second

// publishHeartbeat publishes a heartbeat the way the ingester does.
func publishHeartbeat(t *testing.T, bus *eventstest.Bus, monitorID, msg string) {
	t.Helper()
	assert.NoError(t, events.Publish(context.Background(), bus, events.HeartbeatEvent, heartbeat.Model{MonitorID: monitorID, Msg: msg}))
}

// harness is a server over a database with two workspaces: A holds monitorA and the user userA,
// B holds monitorB and userB.
type harness struct {
	url    string
	bus    *eventstest.Bus
	tokens *auth.TokenMaker
	db     *bun.DB
	userA  string
	userB  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithConnectionDelay(t, 0)
}

// newHarnessWithConnectionDelay is newHarness with the server's connection handler waiting for
// delay first, when it isn't zero.
func newHarnessWithConnectionDelay(t *testing.T, delay time.Duration) *harness {
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
	server, err := websocket.NewServer(&config.Config{}, bus, tokens, workspace.NewSQLRepository(db), monitor.NewSQLRepository(db), logger)
	require.NoError(t, err)
	if delay > 0 {
		websocket.SlowConnectionHandler(server, delay)
	}
	srv := httptest.NewServer(server)
	// Dropping the connections ends the open requests, so Close doesn't wait
	// for them. server.Close isn't called: engine.io then writes to its
	// transports from another goroutine, a data race inside the library.
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	h := &harness{url: srv.URL, bus: bus, tokens: tokens, db: db}
	workspacetest.Add(t, db, workspaceA, "A")
	workspacetest.Add(t, db, workspaceB, "B")
	h.addMonitor(t, workspaceA, monitorA)
	h.addMonitor(t, workspaceB, monitorB)
	h.addMonitor(t, workspaceA, monitorA2)
	h.userA = h.addUser(t, workspaceA)
	h.userB = h.addUser(t, workspaceB)
	return h
}

// addUser makes a user who is a member of the workspace and returns their ID
func (h *harness) addUser(t *testing.T, id workspace.ID) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := h.db.Exec("INSERT INTO users (id, email, password) VALUES (?, ?, 'hash')", userID, userID+"@example.com")
	require.NoError(t, err)
	require.NoError(t, workspace.NewSQLRepository(h.db).AddMember(context.Background(), id, userID, workspace.RoleMember))
	return userID
}

// removeMember takes the user out of the workspace in the database, as the removal does before it
// publishes MemberLeft
func (h *harness) removeMember(t *testing.T, id workspace.ID, userID string) {
	t.Helper()
	_, err := h.db.Exec("DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?", string(id), userID)
	require.NoError(t, err)
}

// addMonitor stores a monitor in the workspace
func (h *harness) addMonitor(t *testing.T, id workspace.ID, monitorID string) {
	t.Helper()
	_, err := h.db.Exec("INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0)", monitorID, string(id))
	require.NoError(t, err)
}

// tokenFor is an access token of the user
func (h *harness) tokenFor(t *testing.T, userID string) string {
	t.Helper()
	token, err := h.tokens.CreateAccessToken(context.Background(), &auth.Model{ID: userID, Email: userID + "@example.com"})
	require.NoError(t, err)
	return token
}

// accessToken is an access token of userA
func (h *harness) accessToken(t *testing.T) string {
	t.Helper()
	return h.tokenFor(t, h.userA)
}

// client speaks Engine.IO v4 over a websocket, the transport the web app uses. The tests don't
// use long polling: when a polling client closes, engine.io's polling transport writes to a
// response the HTTP server has already finished, a data race inside the library that the race job
// reports.
type client struct {
	packets chan string
	// send writes one Engine.IO packet to the server
	send func(packet string) error
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

// answerWithin is how long open waits for the server's answer to CONNECT before it tries again
const answerWithin = 3 * time.Second

// open opens an Engine.IO session over a websocket with the extra query parameters and sends the
// socket.io CONNECT packet. A reader answers pings.
//
// engine.io sends OPEN before it hands the session to socket.io, so a CONNECT that comes back
// quickly can arrive before socket.io listens, and is dropped, a race inside the library that a
// client on a real network is too slow to hit. The server then never answers, so open tries again
// on a new session.
func (h *harness) open(t *testing.T, extra url.Values, connect string) *client {
	t.Helper()
	for attempt := 0; attempt < 5; attempt++ {
		if c := h.tryOpen(t, extra, connect); c != nil {
			return c
		}
	}
	require.FailNow(t, "the server didn't answer CONNECT")
	return nil
}

// tryOpen is one try of open. It returns nil when the server doesn't answer CONNECT in time.
func (h *harness) tryOpen(t *testing.T, extra url.Values, connect string) *client {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"websocket"}}
	for name, values := range extra {
		query[name] = values
	}
	conn, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.url, "http")+"/socket.io/?"+query.Encode(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))
	_, open, err := conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(open), "0{"), "expected Engine.IO OPEN, got %s", open)
	require.NoError(t, conn.SetReadDeadline(time.Time{}))

	// gorilla allows one writer at a time, and the reader answers pings
	var writing sync.Mutex
	c := &client{packets: make(chan string, 256)}
	c.send = func(packet string) error {
		writing.Lock()
		defer writing.Unlock()
		return conn.WriteMessage(gorilla.TextMessage, []byte(packet))
	}
	require.NoError(t, c.send(connect))
	answered := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(c.packets)
		for {
			_, packet, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch string(packet) {
			case "1": // close
				return
			case "2": // ping
				_ = c.send("3")
			case "6": // noop
			default:
				c.packets <- string(packet)
				once.Do(func() { close(answered) })
			}
		}
	}()
	select {
	case <-answered:
		return c
	case <-time.After(answerWithin):
		_ = conn.Close()
		return nil
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

// connect connects as userA, a member of workspace A
func (h *harness) connect(t *testing.T) *client {
	t.Helper()
	return h.connectAs(t, h.userA)
}

func (h *harness) connectAs(t *testing.T, userID string) *client {
	t.Helper()
	c := h.dial(t, h.tokenFor(t, userID))
	packet := c.next(t)
	require.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
	return c
}

func (c *client) emit(t *testing.T, name, arg string) {
	t.Helper()
	packet, err := json.Marshal([]string{name, arg})
	require.NoError(t, err)
	require.NoError(t, c.send("42"+string(packet)))
}

var ackIDs atomic.Uint64

// emitWithAck sends a room event the way a client that wants an answer does, and returns the
// answer: the first argument of the ack. The server answers once it has handled the message, so a
// test that has the answer knows the join or leave is done. Packets that arrive before the answer
// are dropped, so call it before anything is published.
func (c *client) emitWithAck(t *testing.T, name, arg string) map[string]string {
	t.Helper()
	id := strconv.FormatUint(ackIDs.Add(1), 10)
	packet, err := json.Marshal([]string{name, arg})
	require.NoError(t, err)
	require.NoError(t, c.send("42"+id+string(packet)))
	deadline := time.After(wait)
	for {
		select {
		case p, ok := <-c.packets:
			require.True(t, ok, "session closed")
			if !strings.HasPrefix(p, "43"+id+"[") {
				continue
			}
			var args []map[string]string
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(p, "43"+id)), &args), p)
			require.Len(t, args, 1, p)
			return args[0]
		case <-deadline:
			require.FailNow(t, name+" "+arg+" was not answered within "+wait.String())
		}
	}
}

// joinOK joins a room the user may join and waits for the server to say it did
func (c *client) joinOK(t *testing.T, room string) {
	t.Helper()
	assert.Equal(t, map[string]string{"status": "ok"}, c.emitWithAck(t, "join_room", room), "join_room %s", room)
}

// joinRefused asks for a room and checks the server refuses, with the same answer whatever the
// reason, so the answer doesn't tell a room that exists from one that doesn't
func (c *client) joinRefused(t *testing.T, room string) {
	t.Helper()
	assert.Equal(t, map[string]string{"status": "error", "message": "not allowed"}, c.emitWithAck(t, "join_room", room), "join_room %s", room)
}

type rawEvent struct {
	name    string
	payload json.RawMessage
}

// untilEvent reads events until match accepts one and returns every event read, that one
// included. It reads the events of any payload, which until does not.
func (c *client) untilEvent(t *testing.T, what string, match func(rawEvent) bool) []rawEvent {
	t.Helper()
	var seen []rawEvent
	deadline := time.After(wait)
	for {
		var packet string
		select {
		case p, ok := <-c.packets:
			require.True(t, ok, "session closed")
			packet = p
		case <-deadline:
			require.FailNow(t, "no "+what+" within "+wait.String())
		}
		if !strings.HasPrefix(packet, "42") {
			continue
		}
		var args []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(packet[2:]), &args))
		require.Len(t, args, 2, packet)
		var e rawEvent
		require.NoError(t, json.Unmarshal(args[0], &e.name))
		e.payload = args[1]
		seen = append(seen, e)
		if match(e) {
			return seen
		}
	}
}

// heartbeatMsgs are the messages of the heartbeats among the events
func heartbeatMsgs(seen []rawEvent) []string {
	var out []string
	for _, e := range seen {
		var hb heartbeat.Model
		if strings.HasSuffix(e.name, ":heartbeat") && json.Unmarshal(e.payload, &hb) == nil {
			out = append(out, hb.Msg)
		}
	}
	return out
}

// eventNames are the names of the events
func eventNames(seen []rawEvent) []string {
	var out []string
	for _, e := range seen {
		out = append(out, e.name)
	}
	return out
}

// untilMsg reads events until the heartbeat with msg arrives
func (c *client) untilMsg(t *testing.T, msg string) []rawEvent {
	t.Helper()
	return c.untilEvent(t, "heartbeat "+msg, func(e rawEvent) bool {
		var hb heartbeat.Model
		return strings.HasSuffix(e.name, ":heartbeat") && json.Unmarshal(e.payload, &hb) == nil && hb.Msg == msg
	})
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

// join enters a room and waits until it delivers. Socket.io handles each message in its own
// goroutine, so the room isn't joined when the POST returns: the client publishes test heartbeats
// until one arrives. It sends join_room once, so that a room under test gets one join and one leave.
func (c *client) join(t *testing.T, bus *eventstest.Bus, room, monitorID string) {
	t.Helper()
	c.emit(t, "join_room", room)
	c.waitUntilJoined(t, bus, room, monitorID)
}

// waitUntilJoined publishes test heartbeats for monitorID until one arrives on the client.
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

// A client sends join_room as soon as it sees CONNECT, the way the web app does. The server
// registers its handler before it sends CONNECT, so the join is handled even when the connection
// handler runs late.
func TestJoinSentRightAfterConnectIsHandled(t *testing.T) {
	h := newHarnessWithConnectionDelay(t, 300*time.Millisecond)
	c := h.connect(t)

	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
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

// PP-331: the event carries the probe instance that ran the check as instance_id, null when none did.
func TestHeartbeatEventCarriesTheProbeInstance(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
	instanceID := "5b0c7d0e-3f4c-4c47-9a5b-2f1d8d8f6a11"

	assert.NoError(t, events.Publish(context.Background(), h.bus, events.HeartbeatEvent, heartbeat.Model{MonitorID: monitorA, ProbeID: "probe-frankfurt", InstanceID: &instanceID, Msg: "from an instance"}))
	assert.NoError(t, events.Publish(context.Background(), h.bus, events.HeartbeatEvent, heartbeat.Model{MonitorID: monitorA, Msg: "from a push"}))

	want := map[string]string{
		"from an instance": `"instance_id":"` + instanceID + `"`,
		"from a push":      `"instance_id":null`,
	}
	deadline := time.After(wait)
	for len(want) > 0 {
		select {
		case packet, ok := <-c.packets:
			require.True(t, ok, "session closed")
			for msg, field := range want {
				if strings.Contains(packet, msg) {
					assert.Contains(t, packet, field)
					delete(want, msg)
				}
			}
		case <-deadline:
			require.FailNow(t, "no heartbeat within "+wait.String())
		}
	}
}

func TestLeaveRoomStopsDelivery(t *testing.T) {
	h := newHarness(t)
	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
	c.join(t, h.bus, "monitor:"+monitorA2, monitorA2)

	c.emit(t, "leave_room", "monitor:"+monitorA)

	// The leave runs in its own goroutine too, so publish rounds until A's
	// heartbeat stops arriving. B's heartbeat, still subscribed, closes each
	// round.
	deliversA := func(round int) bool {
		a, b := fmt.Sprintf("a-%d", round), fmt.Sprintf("b-%d", round)
		publishHeartbeat(t, h.bus, monitorA, a)
		publishHeartbeat(t, h.bus, monitorA2, b)
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
				require.NoError(t, c.send(`42["`+event+`"`+arg.json+`]`))

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
	// The library closes the session, which may fail this write if it is quick, but the packet
	// has reached the server by then.
	_ = bad.send("42")

	c := h.connect(t)
	c.join(t, h.bus, "monitor:"+monitorA, monitorA)
}

func TestWorkspaceRoomGetsOnlyThatWorkspacesHeartbeats(t *testing.T) {
	h := newHarness(t)
	a, b := h.connectAs(t, h.userA), h.connectAs(t, h.userB)
	a.joinOK(t, roomOf(workspaceA))
	b.joinOK(t, roomOf(workspaceB))

	publishHeartbeat(t, h.bus, monitorA, "a-1")
	publishHeartbeat(t, h.bus, monitorB, "b-1")

	seenByA := a.untilMsg(t, "a-1")
	last := seenByA[len(seenByA)-1]
	assert.Equal(t, heartbeatOf(roomOf(workspaceA)), last.name)
	assert.Contains(t, string(last.payload), `"monitor_id":"`+monitorA+`"`)
	assert.NotContains(t, string(last.payload), "workspace", "the update carries no workspace field")

	// Published after a-1 and delivered in order, so a-1 would have come first
	seenByB := b.untilMsg(t, "b-1")
	assert.Equal(t, []string{"b-1"}, heartbeatMsgs(seenByB), "B gets nothing of A's")
	assert.Equal(t, heartbeatOf(roomOf(workspaceB)), seenByB[len(seenByB)-1].name)
}

func TestMonitorRoomIsOpenToMembersOfItsWorkspace(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, "monitor:"+monitorA)

	publishHeartbeat(t, h.bus, monitorA, "a-1")

	seen := a.untilMsg(t, "a-1")
	assert.Equal(t, heartbeatOf("monitor:"+monitorA), seen[len(seen)-1].name)
}

// A heartbeat goes to its workspace's room and its own monitor's room, and to no other
func TestAHeartbeatGoesToItsWorkspaceRoomAndItsMonitorRoom(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, roomOf(workspaceA))
	a.joinOK(t, "monitor:"+monitorA)

	publishHeartbeat(t, h.bus, monitorA, "a-1")

	// The two events come one after the other: read until both are in
	got := 0
	seen := a.untilEvent(t, "both events of heartbeat a-1", func(e rawEvent) bool {
		if strings.Contains(string(e.payload), `"a-1"`) {
			got++
		}
		return got == 2
	})
	var names []string
	for _, e := range seen {
		names = append(names, e.name)
	}
	assert.ElementsMatch(t, []string{heartbeatOf(roomOf(workspaceA)), heartbeatOf("monitor:" + monitorA)}, names)
}

func TestJoinsOfRoomsTheUserMayNotEnterAreRefused(t *testing.T) {
	h := newHarness(t)
	b := h.connectAs(t, h.userB)
	b.joinOK(t, roomOf(workspaceB))

	for name, room := range map[string]string{
		"another workspace":              roomOf(workspaceA),
		"another workspace's monitor":    "monitor:" + monitorA,
		"the old install-wide room":      "monitor:all",
		"a monitor that doesn't exist":   "monitor:" + uuid.NewString(),
		"a monitor ID that isn't a UUID": "monitor:abc",
		"a workspace ID that isn't one":  "workspace:abc",
		"a workspace that doesn't exist": roomOf(workspace.ID(uuid.NewString())),
		"a workspace with no ID":         "workspace:",
		"a monitor with no ID":           "monitor:",
		"the install room":               "install",
		"a room of a user":               "user:" + h.userA,
		"an unknown prefix":              "team:" + string(workspaceB),
		"no prefix":                      string(workspaceB),
		"an empty name":                  "",
	} {
		t.Run(name, func(t *testing.T) { b.joinRefused(t, room) })
	}

	// Every join above has been answered, so a refused one is done: none of A's updates reach B
	publishHeartbeat(t, h.bus, monitorA, "a-1")
	publishHeartbeat(t, h.bus, monitorB, "b-1")
	seen := b.untilMsg(t, "b-1")
	assert.Equal(t, []string{"b-1"}, heartbeatMsgs(seen))
}

// An upper-case copy of a member's own workspace ID is the same workspace: the join is accepted and
// the room is the one the heartbeats go to
func TestAnUpperCaseIDJoinsTheRoomOfTheLowerCaseOne(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, "workspace:"+strings.ToUpper(string(workspaceA)))
	a.joinOK(t, "monitor:"+strings.ToUpper(monitorA))

	publishHeartbeat(t, h.bus, monitorA, "a-1")
	got := 0
	seen := a.untilEvent(t, "both events of heartbeat a-1", func(e rawEvent) bool {
		if strings.Contains(string(e.payload), `"a-1"`) {
			got++
		}
		return got == 2
	})
	var names []string
	for _, e := range seen {
		names = append(names, e.name)
	}
	assert.ElementsMatch(t, []string{heartbeatOf(roomOf(workspaceA)), heartbeatOf("monitor:" + monitorA)}, names)

	// leave_room takes the same form
	require.Equal(t, map[string]string{"status": "ok"}, a.emitWithAck(t, "leave_room", "workspace:"+strings.ToUpper(string(workspaceA))))
	publishHeartbeat(t, h.bus, monitorA, "a-2")
	seen = a.untilMsg(t, "a-2")
	assert.Equal(t, []string{heartbeatOf("monitor:" + monitorA)}, eventNames(seen))
}

// A malformed ID is refused whatever the prefix, and a user of no workspace is refused the same way
func TestAMalformedIDIsRefused(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	for _, room := range []string{"monitor:abc", "monitor:all", "workspace:abc", "workspace:" + string(workspaceA) + "x", "monitor: " + monitorA} {
		a.joinRefused(t, room)
	}
}

// A user in no workspace can join none
func TestAUserInNoWorkspaceJoinsNoRoom(t *testing.T) {
	h := newHarness(t)
	h.removeMember(t, workspaceA, h.userA)
	a := h.connectAs(t, h.userA)

	a.joinRefused(t, roomOf(workspaceA))
	a.joinRefused(t, "monitor:"+monitorA)
}

// The membership is checked when the user joins, not when they connect
func TestAUserRemovedBeforeTheyJoinIsRefused(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	h.removeMember(t, workspaceA, h.userA)

	a.joinRefused(t, roomOf(workspaceA))
	a.joinRefused(t, "monitor:"+monitorA)
}

// Nothing about a probe goes over the websocket: a probe going offline or coming back reaches no
// socket, so nothing install-wide is sent to the members of every workspace
func TestAProbeGoingOfflineSendsNothingToAMember(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, roomOf(workspaceA))

	since := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	require.NoError(t, events.Publish(context.Background(), h.bus, probe.ProbeOffline, probe.Event{ProbeID: "probe-frankfurt", ProbeName: "Frankfurt", Since: since}))
	require.NoError(t, events.Publish(context.Background(), h.bus, probe.ProbeOnline, probe.Event{ProbeID: "probe-frankfurt", ProbeName: "Frankfurt", Since: since}))
	// The heartbeat closes the read: whatever the probe events sent comes before it
	publishHeartbeat(t, h.bus, monitorA, "a-after")

	seen := a.untilMsg(t, "a-after")
	assert.Equal(t, []string{heartbeatOf(roomOf(workspaceA))}, eventNames(seen))
}

// A member who leaves a workspace stops getting its updates on the sockets they have open, with no
// reconnect, and the others of the workspace keep getting them
func TestAMemberWhoLeavesStopsGettingTheWorkspacesUpdates(t *testing.T) {
	h := newHarness(t)
	otherInA := h.addUser(t, workspaceA)
	// Two sockets of the member who leaves, one with a monitor room, and a teammate's
	first, second, teammate := h.connectAs(t, h.userA), h.connectAs(t, h.userA), h.connectAs(t, otherInA)
	// The member who leaves A stays in B
	require.NoError(t, workspace.NewSQLRepository(h.db).AddMember(context.Background(), workspaceB, h.userA, workspace.RoleMember))
	first.joinOK(t, roomOf(workspaceB))
	second.joinOK(t, roomOf(workspaceB))
	first.joinOK(t, roomOf(workspaceA))
	first.joinOK(t, "monitor:"+monitorA)
	second.joinOK(t, roomOf(workspaceA))
	teammate.joinOK(t, roomOf(workspaceA))

	h.removeMember(t, workspaceA, h.userA)
	require.NoError(t, events.Publish(context.Background(), h.bus, workspace.MemberLeft, workspace.MemberLeftEvent{WorkspaceID: workspaceA, UserID: h.userA}))

	publishHeartbeat(t, h.bus, monitorA, "a-after")
	// The heartbeat of workspace B closes the read: the member who left is in B's room still, and
	// it comes after the one of A
	publishHeartbeat(t, h.bus, monitorB, "b-after")

	for name, c := range map[string]*client{"first socket": first, "second socket": second} {
		t.Run(name, func(t *testing.T) {
			seen := c.untilMsg(t, "b-after")
			assert.Equal(t, []string{"b-after"}, heartbeatMsgs(seen), "the member who left gets no update of the workspace")
		})
	}
	t.Run("a teammate", func(t *testing.T) {
		seen := teammate.untilMsg(t, "a-after")
		assert.Equal(t, []string{"a-after"}, heartbeatMsgs(seen))
	})
}

// Leaving one workspace leaves the user's rooms of other workspaces alone
func TestLeavingOneWorkspaceKeepsTheUsersOtherWorkspaces(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, workspace.NewSQLRepository(h.db).AddMember(context.Background(), workspaceB, h.userA, workspace.RoleMember))
	c := h.connectAs(t, h.userA)
	c.joinOK(t, roomOf(workspaceA))
	c.joinOK(t, roomOf(workspaceB))
	c.joinOK(t, "monitor:"+monitorA)
	c.joinOK(t, "monitor:"+monitorB)

	h.removeMember(t, workspaceA, h.userA)
	require.NoError(t, events.Publish(context.Background(), h.bus, workspace.MemberLeft, workspace.MemberLeftEvent{WorkspaceID: workspaceA, UserID: h.userA}))

	publishHeartbeat(t, h.bus, monitorA, "a-after")
	publishHeartbeat(t, h.bus, monitorB, "b-after")
	got := 0
	var names []string
	for _, e := range c.untilEvent(t, "both events of heartbeat b-after", func(e rawEvent) bool {
		if strings.Contains(string(e.payload), `"b-after"`) {
			got++
		}
		return got == 2
	}) {
		names = append(names, e.name)
	}
	assert.NotContains(t, names, heartbeatOf(roomOf(workspaceA)))
	assert.NotContains(t, names, heartbeatOf("monitor:"+monitorA))
	assert.Contains(t, names, heartbeatOf(roomOf(workspaceB)))
}

// A MemberLeft for a user with no open socket, or one that names nothing, changes nothing
func TestMemberLeftForAUserWithNoSocketIsHarmless(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, roomOf(workspaceA))

	require.NoError(t, events.Publish(context.Background(), h.bus, workspace.MemberLeft, workspace.MemberLeftEvent{WorkspaceID: workspaceA, UserID: "nobody"}))
	require.NoError(t, events.Publish(context.Background(), h.bus, workspace.MemberLeft, workspace.MemberLeftEvent{}))

	publishHeartbeat(t, h.bus, monitorA, "a-1")
	a.untilMsg(t, "a-1")
}
