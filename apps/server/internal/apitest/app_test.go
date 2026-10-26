// Package apitest drives the API router that cmd/api serves, with real
// services over SQLite and an in-process Redis, through httptest.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal"
	wiring "peekaping/internal/app" // app is this package's server under test
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/setting"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/dig"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "Correct-horse-1"

	// maxFailedLogins is how many failed logins lock an email from one IP,
	// the same as the BRUTEFORCE_MAX_ATTEMPTS default.
	maxFailedLogins = 20

	// maxFailedLoginsPerAccount is how many failed logins lock an email whatever the IP, the
	// same as the BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS default.
	maxFailedLoginsPerAccount = 100

	// clientURL is the CLIENT_URL of the server under test: the web app's address.
	clientURL = "https://peek.example/status"
)

// app is one API server with its own database and Redis.
type app struct {
	router    *httpx.Router
	container *dig.Container
	redis     *miniredis.Miniredis
	cfg       *config.Config

	accessToken string
	apiKey      string
}

// withMetrics turns on the /metrics endpoint, which is off by default.
func withMetrics(cfg *config.Config) { cfg.MetricsEnabled = true }

// withCORSOrigins sets CORS_ORIGINS, which is unset by default.
func withCORSOrigins(list string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.CORSOrigins = list }
}

// newApp builds an API server on the default config, changed by options.
func newApp(t *testing.T, options ...func(*config.Config)) *app {
	t.Helper()
	return newAppDecorated(t, nil, options...)
}

// newAppDecorated is newApp with decorate run on the container once the API's dependencies are
// registered and before anything is built from them, so that it can swap one of them with
// container.Decorate.
func newAppDecorated(t *testing.T, decorate func(*dig.Container) error, options ...func(*config.Config)) *app {
	t.Helper()
	ctx := context.Background()
	validation.RegisterCustomValidators()

	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	cfg := &config.Config{
		DBType:                "sqlite",
		DBName:                filepath.Join(t.TempDir(), "api.db"),
		ClientURL:             clientURL,
		Mode:                  "test",
		LogLevel:              "fatal",
		Timezone:              "UTC",
		RedisHost:             redisHost,
		RedisPort:             redisPort,
		BruteforceMaxAttempts: maxFailedLogins,
		BruteforceWindow:      time.Minute,
		BruteforceLockout:     time.Minute,

		BruteforceAccountMaxAttempts: maxFailedLoginsPerAccount,
		BruteforceAccountWindow:      10 * time.Minute,
		BruteforceAccountLockout:     time.Minute,

		ServiceName: "peekaping:apitest",
	}
	for _, option := range options {
		option(cfg)
	}

	container := dig.New()
	require.NoError(t, wiring.RegisterClusterAPI(container, cfg))
	if decorate != nil {
		require.NoError(t, decorate(container))
	}
	require.NoError(t, container.Invoke(func(db *bun.DB, bus events.EventBus, queue *asynq.Client) error {
		t.Cleanup(func() {
			require.NoError(t, bus.Close(context.Background()))
			require.NoError(t, queue.Close())
			require.NoError(t, db.Close())
		})
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		if err := migrator.Init(ctx); err != nil {
			return err
		}
		_, err := migrator.Migrate(ctx)
		return err
	}))
	require.NoError(t, container.Invoke(func(settings setting.Service) error {
		return settings.InitializeSettings(ctx)
	}))
	// PROBE_BOOTSTRAP_TOKEN is applied after the settings initialise, as cmd/api does at startup.
	require.NoError(t, wiring.BootstrapLocalProbe(ctx, container))
	// Notification channel providers register when the listener is built, as cmd/api does at startup.
	require.NoError(t, container.Invoke(func(listener *notification_channel.NotificationEventListener, bus events.EventBus) {
		listener.Subscribe(bus)
	}))
	// The waker ends the probes' waiting assignment polls when a monitor or an instance changes, as it does in cmd/api.
	require.NoError(t, container.Invoke(func(waker *protocol.Waker) { waker.Subscribe() }))
	// The metrics collector starts when the config asks for it, as cmd/api does. It stops before the event bus closes.
	if cfg.MetricsEnabled {
		require.NoError(t, container.Invoke(func(collector *metrics.Collector, bus events.EventBus) {
			metricsCtx, stopMetrics := context.WithCancel(ctx)
			t.Cleanup(stopMetrics)
			collector.Start(metricsCtx, bus)
		}))
	}

	a := &app{container: container, redis: redis, cfg: cfg}
	require.NoError(t, container.Invoke(func(server *internal.Server) { a.router = server.Router }))
	return a
}

// newReplica is another API replica of the install that first is a replica of: the same database
// and the same Redis, with its own process memory. options change the config as they do for newApp.
// The admin's access token, which the replicas share through the database, is first's.
func newReplica(t *testing.T, first *app, options ...func(*config.Config)) *app {
	t.Helper()
	share := func(cfg *config.Config) {
		cfg.DBName = first.cfg.DBName
		cfg.RedisHost = first.cfg.RedisHost
		cfg.RedisPort = first.cfg.RedisPort
	}
	replica := newApp(t, append(options, share)...)
	// newApp started a Redis of its own that the replica doesn't use: point at the shared one, so
	// that fast-forwarding the replica's Redis moves the one it talks to
	replica.redis = first.redis
	replica.accessToken, replica.apiKey = first.accessToken, first.apiKey
	return replica
}

// newAppWithAdmin registers the admin and issues an API key, the way a new
// install is set up.
func newAppWithAdmin(t *testing.T, options ...func(*config.Config)) *app {
	t.Helper()
	a := newApp(t, options...)
	a.registerAdmin(t)
	return a
}

// inspector inspects the queues of the app's Redis, which the app itself has no reason to
func (a *app) inspector(t *testing.T) *asynq.Inspector {
	t.Helper()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: a.redis.Addr()})
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	return inspector
}

// registerAdmin registers the admin and issues an API key.
func (a *app) registerAdmin(t *testing.T) {
	t.Helper()
	registered := decode[struct {
		AccessToken string `json:"accessToken"`
	}](t, a.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"email": adminEmail, "password": adminPassword,
	}), http.StatusCreated)
	a.accessToken = registered.AccessToken

	created := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]string{"name": "apitest"}, a.asAdmin), http.StatusCreated)
	a.apiKey = created.Token
}

// credentials sets the authentication headers on a request.
type credentials func(*http.Request)

func (a *app) asAdmin(req *http.Request) { req.Header.Set("Authorization", "Bearer "+a.accessToken) }

func (a *app) withAPIKey(req *http.Request) { req.Header.Set("X-API-Key", a.apiKey) }

// request sends body as JSON (or as-is when it's a string) and returns the
// response.
func (a *app) request(t *testing.T, method, path string, body any, creds ...credentials) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		encoded, err := json.Marshal(b)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for _, apply := range creds {
		apply(req)
	}
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, req)
	return w
}

// decode checks the status code and returns the response's data field.
func decode[T any](t *testing.T, w *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	require.Equal(t, wantStatus, w.Code, w.Body.String())
	var envelope utils.ApiResponse[T]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), w.Body.String())
	return envelope.Data
}

// call is one request in a table test.
type call struct {
	name, method, path string
	body               any
}

// expectStatus sends each call as the admin and checks its status code.
func (a *app) expectStatus(t *testing.T, status int, calls []call) {
	t.Helper()
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			w := a.request(t, c.method, c.path, c.body, a.asAdmin)
			assert.Equal(t, status, w.Code, w.Body.String())
		})
	}
}
