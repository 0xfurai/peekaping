package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "Correct-horse-1"
	// wait is how long a test waits for something the app does on its own
	wait = 10 * time.Second
)

// loadConfig loads peekaping's config from the environment, with a SQLite database of its own,
// changed by env. The REDIS_* variables, WEB_DIR and API_URL are unset unless env sets them.
func loadConfig(t *testing.T, env map[string]string) *Config {
	t.Helper()
	validation.RegisterCustomValidators()
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	t.Setenv("MODE", "test")
	t.Setenv("METRICS_ENABLED", "")
	t.Setenv("WEB_DIR", "")
	t.Setenv("API_URL", "")
	// The usage ping and error reports are on by default, so no test may reach the real endpoints
	t.Setenv("TELEMETRY_URL", "http://127.0.0.1:9/v1/ping")
	t.Setenv("ERROR_REPORTING_DSN", "http://public@127.0.0.1:9/1")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg
}

// migrateDatabase migrates the database before peekaping starts, as the startup script does
// with cmd/bun
func migrateDatabase(t testing.TB, cfg *Config) {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: cfg.DBType, DBName: cfg.DBName}, zap.NewNop().Sugar())
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
}

// instance is a peekaping running in the test, on a listener of its own
type instance struct {
	url    string
	logs   *observer.ObservedLogs
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error

	accessToken string
}

// startPeekaping migrates the database and runs peekaping with a logger the test observes. It
// returns once the API answers. The test's cleanup stops it if the test didn't.
func startPeekaping(t testing.TB, cfg *Config, logger *zap.SugaredLogger, logs *observer.ObservedLogs) *instance {
	t.Helper()
	// The usage ping and error reports are on by default, so a test that starts peekaping must
	// point them at a server of its own (loadConfig points them nowhere)
	require.NotContains(t, cfg.TelemetryURL, "peekaping.com", "a test must not reach the real telemetry endpoint")
	require.NotEmpty(t, cfg.ErrorReportingDSN, "an empty DSN means Peekaping's own Sentry project")
	require.NotContains(t, cfg.ErrorReportingDSN, "sentry.io", "a test must not reach the real Sentry project")
	migrateDatabase(t, cfg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{
		url:    "http://" + listener.Addr().String(),
		logs:   logs,
		cancel: cancel,
		done:   make(chan error, 1),
	}
	go func() { in.done <- run(ctx, cfg, logger, listener) }()
	t.Cleanup(func() { _ = in.stop(t) })

	deadline := time.Now().Add(wait)
	for {
		resp, err := http.Get(in.url + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return in
			}
		}
		select {
		case err := <-in.done:
			require.FailNow(t, "peekaping stopped while starting", "%v", err)
		case <-time.After(20 * time.Millisecond):
		}
		require.True(t, time.Now().Before(deadline), "peekaping didn't answer within %s", wait)
	}
}

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

// stop cancels run's context, as SIGTERM does, and returns what run returned. It fails the test
// if run takes longer than the shutdown deadline.
func (in *instance) stop(t testing.TB) error {
	t.Helper()
	in.stopOnce.Do(func() {
		in.cancel()
		limit := shutdownTimeouts.Total() + 5*time.Second
		select {
		case in.stopErr = <-in.done:
		case <-time.After(limit):
			in.stopErr = fmt.Errorf("run didn't return within %s", limit)
			t.Error(in.stopErr)
		}
	})
	return in.stopErr
}

// sendHalfALogin sends the admin's login with only part of its body, and returns the connection
// and the rest of the body. The server answers once the rest arrives.
func (in *instance) sendHalfALogin(t *testing.T) (net.Conn, string) {
	t.Helper()
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, adminPassword)
	conn, err := net.Dial("tcp", strings.TrimPrefix(in.url, "http://"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST /api/v1/auth/login HTTP/1.1\r\nHost: peekaping\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		len(login), login[:10])
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond) // the server reads the headers and waits for the rest
	return conn, login[10:]
}

// lockDatabase holds the SQLite database's write lock from a connection of its own, as another
// writer would, until unlock is called. The app's writes wait for it.
func lockDatabase(t *testing.T, path string) (unlock func()) {
	t.Helper()
	ctx := context.Background()
	// Without the app's shared cache, so that the two connections lock each other out
	db, err := sql.Open(sqliteshim.ShimName, "file:"+path+"?mode=rw")
	require.NoError(t, err)
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "PRAGMA busy_timeout=5000")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)

	var once sync.Once
	unlock = func() {
		once.Do(func() {
			_, err := conn.ExecContext(ctx, "ROLLBACK")
			assert.NoError(t, err)
			assert.NoError(t, conn.Close())
			assert.NoError(t, db.Close())
		})
	}
	t.Cleanup(unlock)
	return unlock
}

// countHeartbeats counts the monitor's heartbeats in the database, once peekaping has closed it
func countHeartbeats(t *testing.T, path, monitorID string) int {
	t.Helper()
	db, err := sql.Open(sqliteshim.ShimName, "file:"+path+"?mode=ro")
	require.NoError(t, err)
	defer db.Close()
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM heartbeats WHERE monitor_id = ?", monitorID).Scan(&count))
	return count
}

// request sends body as JSON with the admin's access token, if any, and returns the response's
// status code and body
func (in *instance) request(t testing.TB, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, in.url+path, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if in.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+in.accessToken)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// decode checks the status code and returns the response's data field
func decode[T any](t testing.TB, status int, body []byte, wantStatus int) T {
	t.Helper()
	require.Equal(t, wantStatus, status, string(body))
	var envelope utils.ApiResponse[T]
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	return envelope.Data
}

// registerAdmin registers the admin, as a new install's first setup does, and sends the
// requests that follow with the admin's access token
func (in *instance) registerAdmin(t testing.TB) {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": adminEmail, "password": adminPassword,
	})
	in.accessToken = decode[struct {
		AccessToken string `json:"accessToken"`
	}](t, status, body, http.StatusCreated).AccessToken
}

// createMonitor creates a monitor through the API and returns its ID
func (in *instance) createMonitor(t testing.TB, monitor map[string]any) string {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/monitors", monitor)
	return decode[struct {
		ID string `json:"id"`
	}](t, status, body, http.StatusCreated).ID
}

// httpMonitor is an HTTP monitor of the URL, checked every interval seconds
func httpMonitor(name, target string, interval int) map[string]any {
	return map[string]any{
		"type":             "http",
		"name":             name,
		"interval":         interval,
		"retry_interval":   interval,
		"timeout":          16,
		"active":           true,
		"notification_ids": []string{},
		"config":           fmt.Sprintf(`{"url":%q,"method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`, target),
	}
}

// pushMonitor is a push monitor whose push URL has the token
func pushMonitor(name, pushToken string) map[string]any {
	return map[string]any{
		"type":             "push",
		"name":             name,
		"interval":         60,
		"retry_interval":   60,
		"timeout":          16,
		"active":           true,
		"notification_ids": []string{},
		"push_token":       pushToken,
		"config":           fmt.Sprintf(`{"pushToken":%q}`, pushToken),
	}
}

// heartbeats returns the monitor's latest heartbeats, newest first, through the API
func (in *instance) heartbeats(t testing.TB, monitorID string) []heartbeat.Model {
	t.Helper()
	status, body := in.request(t, http.MethodGet, "/api/v1/monitors/"+monitorID+"/heartbeats?limit=100", nil)
	return decode[[]heartbeat.Model](t, status, body, http.StatusOK)
}

// countingListener accepts TCP connections and counts them, as a Redis server would see them
func countingListener(t *testing.T) (host, port string, accepted func() int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	var count atomic.Int64
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			count.Add(1)
			_ = conn.Close()
		}
	}()
	host, port, err = net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	return host, port, count.Load
}

// socketClient speaks Engine.IO v4 long polling, the transport a socket.io client starts with.
// A reader keeps one long poll open, answers pings, and passes on the socket.io events.
type socketClient struct {
	url     string
	packets chan string
	// polls counts the long polls that got an answer
	polls atomic.Int64
}

// socketEvent is a heartbeat the websocket pushed
type socketEvent struct {
	name string
	hb   heartbeat.Model
}

// socketConnect is the socket.io CONNECT packet of a client that sends the admin's access token
// in its auth payload, where the websocket reads it.
func (in *instance) socketConnect(t *testing.T) string {
	t.Helper()
	auth, err := json.Marshal(map[string]string{"token": in.accessToken})
	require.NoError(t, err)
	return "40" + string(auth)
}

// dialSocket opens a socket.io session with the admin's access token. The test's cleanup closes
// it.
func (in *instance) dialSocket(t *testing.T) *socketClient {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"polling"}}
	base := in.url + "/socket.io/?" + query.Encode()
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

	c := &socketClient{url: base + "&sid=" + url.QueryEscape(open.SID), packets: make(chan string, 256)}
	require.NoError(t, c.post(in.socketConnect(t)))
	go c.read()
	t.Cleanup(c.close)

	select {
	case packet := <-c.packets:
		require.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
	case <-time.After(wait):
		require.FailNow(t, "no CONNECT within "+wait.String())
	}
	return c
}

func (c *socketClient) post(packet string) error {
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

func (c *socketClient) read() {
	defer close(c.packets)
	for {
		resp, err := http.Get(c.url)
		if err != nil {
			return
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.polls.Add(1)
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

// close ends the session, which answers its open long poll
func (c *socketClient) close() {
	_ = c.post("1")
	for range c.packets {
	}
}

// dialWebsocket opens a socket.io session over a websocket, the transport a socket.io client
// upgrades to, with the admin's access token. The test's cleanup closes it.
func (in *instance) dialWebsocket(t *testing.T) *gorilla.Conn {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"websocket"}}
	conn, resp, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(in.url, "http")+"/socket.io/?"+query.Encode(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))

	_, open, err := conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(open), "0{"), "Engine.IO OPEN: %s", open)
	require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(in.socketConnect(t))))
	_, connect, err := conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(connect), "40"), "socket.io CONNECT: %s", connect)
	return conn
}

// join enters a room
func (c *socketClient) join(t *testing.T, room string) {
	t.Helper()
	packet, err := json.Marshal([]string{"join_room", room})
	require.NoError(t, err)
	require.NoError(t, c.post("42"+string(packet)))
}

// until reads heartbeats until one matches and returns it
func (c *socketClient) until(t *testing.T, timeout time.Duration, match func(socketEvent) bool) (socketEvent, bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		var packet string
		select {
		case p, ok := <-c.packets:
			require.True(t, ok, "session closed")
			packet = p
		case <-deadline:
			return socketEvent{}, false
		}
		if !strings.HasPrefix(packet, "42") {
			continue
		}
		var args []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(packet[2:]), &args))
		require.Len(t, args, 2, packet)
		var e socketEvent
		require.NoError(t, json.Unmarshal(args[0], &e.name))
		require.NoError(t, json.Unmarshal(args[1], &e.hb))
		if match(e) {
			return e, true
		}
	}
}
