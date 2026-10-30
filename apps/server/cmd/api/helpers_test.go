package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"github.com/alicebob/miniredis/v2"
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
	// connectWait is how long a socket waits for the answer to its socket.io CONNECT before it
	// dials again (see dialWebsocket)
	connectWait = 2 * time.Second
)

// testHTTP makes the tests' requests to the API, each bound by a timeout: a request the API
// never answers fails its test with the goroutines' stacks instead of hanging until the Go test
// timeout, which CI's job limit can beat, leaving no stack at all
var testHTTP = &http.Client{Timeout: 3 * wait}

// pollHTTP makes the socket clients' long polls, which the server answers at the latest with its
// next ping, 25 seconds away
var pollHTTP = &http.Client{Timeout: time.Minute}

// goroutines returns the stacks of every goroutine, for a test that gave up waiting
func goroutines() string {
	buf := make([]byte, 8<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

// loadConfig loads the API's config from the environment, with a SQLite database and an
// in-process Redis of its own, changed by env
func loadConfig(t *testing.T, env map[string]string) *Config {
	t.Helper()
	validation.RegisterCustomValidators()
	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	t.Setenv("REDIS_HOST", redisHost)
	t.Setenv("REDIS_PORT", redisPort)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	t.Setenv("MODE", "test")
	t.Setenv("METRICS_ENABLED", "")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg
}

// migrateDatabase migrates the database before the API starts, as the startup script does with
// cmd/bun
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

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

// instance is an API running in the test, on a listener of its own
type instance struct {
	url    string
	logs   *observer.ObservedLogs
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error

	accessToken string
}

// startAPI migrates the database and runs the API with a logger the test observes. It returns
// once the API answers. The test's cleanup stops it if the test didn't.
func startAPI(t testing.TB, cfg *Config, logger *zap.SugaredLogger, logs *observer.ObservedLogs) *instance {
	t.Helper()
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
		resp, err := testHTTP.Get(in.url + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return in
			}
		}
		select {
		case err := <-in.done:
			// Kept for stop, so that the cleanup doesn't wait for a result that already came
			in.stopOnce.Do(func() { in.cancel(); in.stopErr = err })
			require.FailNow(t, "the API stopped while starting", "%v", err)
		case <-time.After(20 * time.Millisecond):
		}
		require.True(t, time.Now().Before(deadline), "the API didn't answer within %s", wait)
	}
}

// stop cancels run's context, as SIGTERM does, and returns what run returned. It fails the test
// if run takes longer than the shutdown deadline.
func (in *instance) stop(t testing.TB) error {
	t.Helper()
	in.stopOnce.Do(func() {
		// The client may hold a connection it dialed and never sent a request on, when a request
		// went out on another one that came free first. The HTTP server's Shutdown waits up to 5
		// seconds for such a connection.
		testHTTP.CloseIdleConnections()
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
	resp, err := testHTTP.Do(req)
	if err != nil {
		// the stacks only on failure: collecting them takes an 8 MiB buffer per call
		require.NoError(t, err, "%s %s\n%s", method, path, goroutines())
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// registerAdmin registers the admin, as a new install's first setup does, and sends the
// requests that follow with the admin's access token
func (in *instance) registerAdmin(t testing.TB) {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": adminEmail, "password": adminPassword,
	})
	require.Equal(t, http.StatusCreated, status, string(body))
	var envelope utils.ApiResponse[struct {
		AccessToken string `json:"accessToken"`
	}]
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	in.accessToken = envelope.Data.AccessToken
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

// createWaitingForTheDatabase sends the creation of a monitor, whose handler then waits for the
// database's write lock, held by another connection until unlock is called. The HTTP server's
// Close drops the request's connection, which cancels the handler's context but doesn't stop its
// wait: once unlocked, the handler fails, logging "Failed to create monitor".
func (in *instance) createWaitingForTheDatabase(t *testing.T, cfg *Config) (unlock func()) {
	t.Helper()
	body, err := json.Marshal(pushMonitor("Waiting", "waiting-push-token"))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, in.url+"/api/v1/monitors", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.accessToken)

	unlock = lockDatabase(t, cfg.DBName)
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	t.Cleanup(func() {
		select {
		case <-answered:
		case <-time.After(shutdownTimeouts.Total() + wait):
			t.Errorf("the request waiting for the database was never answered\n%s", goroutines())
		}
	})
	time.Sleep(500 * time.Millisecond) // the handler gets to its write
	return unlock
}

// socketClient speaks Engine.IO v4 long polling, the transport a socket.io client starts with.
// A reader keeps one long poll open, answers pings, and passes on the socket.io events.
type socketClient struct {
	t       testing.TB
	url     string
	packets chan string
	// stop ends the reader's long poll
	stop context.CancelFunc
	ctx  context.Context
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
// it. A CONNECT that gets no answer is dialed again, as for dialWebsocket.
func (in *instance) dialSocket(t *testing.T) *socketClient {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		if c := in.trySocket(t); c != nil {
			t.Cleanup(c.close)
			return c
		}
		require.True(t, time.Now().Before(deadline), "no CONNECT within %s\n%s", wait, goroutines())
		t.Logf("no CONNECT within %s, dialing again", connectWait)
	}
}

// trySocket opens a long-polling session and sends its CONNECT. It returns the client once the
// CONNECT is answered, or closes the session and returns nil when it isn't within connectWait.
func (in *instance) trySocket(t *testing.T) *socketClient {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"polling"}}
	base := in.url + "/socket.io/?" + query.Encode()
	resp, err := testHTTP.Get(base)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.True(t, strings.HasPrefix(string(body), "0"), "handshake: %s", body)
	var open struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal(body[1:], &open))

	c := &socketClient{t: t, url: base + "&sid=" + url.QueryEscape(open.SID), packets: make(chan string, 256)}
	c.ctx, c.stop = context.WithCancel(context.Background())
	require.NoError(t, c.post(in.socketConnect(t)))
	go c.read()

	select {
	case packet, ok := <-c.packets:
		require.True(t, ok, "the session ended before its CONNECT was answered")
		require.True(t, strings.HasPrefix(packet, "40"), "expected CONNECT, got %s", packet)
		return c
	case <-time.After(connectWait):
		c.close()
		return nil
	}
}

func (c *socketClient) post(packet string) error {
	resp, err := testHTTP.Post(c.url, "text/plain;charset=UTF-8", strings.NewReader(packet))
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
		req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, c.url, nil)
		if err != nil {
			return
		}
		resp, err := pollHTTP.Do(req)
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

// close ends the session and waits for the reader to stop. The close doesn't always answer the
// reader's long poll: one the server takes in while the session closes is never answered, and
// would hold the reader until the server stops, which the test's cleanup does only after this. So
// the reader's poll is ended here.
func (c *socketClient) close() {
	_ = c.post("1")
	c.stop()
	deadline := time.After(pollHTTP.Timeout + wait)
	for {
		select {
		case _, ok := <-c.packets:
			if !ok {
				return
			}
		case <-deadline:
			c.t.Errorf("the socket's reader didn't stop after the session was closed\n%s", goroutines())
			return
		}
	}
}

// dialWebsocket opens a socket.io session over a websocket, the transport a socket.io client
// upgrades to, with the admin's access token. The test's cleanup closes it.
//
// socket.io (zishang520/socket.io v2.4.11 on engine.io v2.5.0) can drop a CONNECT that comes right
// after the OPEN: the transport reads from the connection before the handshake has given the
// session to socket.io, so a CONNECT read in between reaches no listener and is never answered.
// A socket.io client gives up after its connect timeout and connects again; so does this one,
// after connectWait, on a new connection.
func (in *instance) dialWebsocket(t *testing.T) *gorilla.Conn {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		if conn := in.tryWebsocket(t); conn != nil {
			t.Cleanup(func() { _ = conn.Close() })
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))
			return conn
		}
		require.True(t, time.Now().Before(deadline), "no CONNECT within %s\n%s", wait, goroutines())
		t.Logf("no CONNECT within %s, dialing again", connectWait)
	}
}

// tryWebsocket opens a websocket session and sends its CONNECT. It returns the connection once the
// CONNECT is answered, or closes it and returns nil when it isn't within connectWait.
func (in *instance) tryWebsocket(t *testing.T) *gorilla.Conn {
	t.Helper()
	query := url.Values{"EIO": {"4"}, "transport": {"websocket"}}
	conn, resp, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(in.url, "http")+"/socket.io/?"+query.Encode(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))

	_, open, err := conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(open), "0{"), "Engine.IO OPEN: %s", open)
	require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(in.socketConnect(t))))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(connectWait)))
	_, connect, err := conn.ReadMessage()
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		_ = conn.Close()
		return nil
	}
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(connect), "40"), "socket.io CONNECT: %s", connect)
	return conn
}

// freeMetricsPort is a port nothing listens on, for METRICS_PORT: run opens that listener itself
func freeMetricsPort(t testing.TB) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, listener.Close()) }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	return port
}

// get returns the status and body of a GET to the url
func get(t testing.TB, url string) (int, string) {
	t.Helper()
	resp, err := testHTTP.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}
