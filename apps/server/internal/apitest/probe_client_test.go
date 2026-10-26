package apitest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal"
	wiring "peekaping/internal/app" // app is this package's server under test
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeclient"
	"peekaping/internal/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// newSingleApp is an app that runs as peekaping does, in one process: the results a probe posts
// are stored as heartbeats at once, where cmd/api would queue them for the ingester. Nothing
// checks the monitors of the local probe, since the app's scheduler is not started.
func newSingleApp(t *testing.T) *app {
	t.Helper()
	ctx := context.Background()
	validation.RegisterCustomValidators()
	cfg := &config.Config{
		DBType:                "sqlite",
		DBName:                filepath.Join(t.TempDir(), "single.db"),
		ClientURL:             clientURL,
		Mode:                  "test",
		LogLevel:              "fatal",
		Timezone:              "UTC",
		BruteforceMaxAttempts: maxFailedLogins,
		BruteforceWindow:      time.Minute,
		BruteforceLockout:     time.Minute,

		BruteforceAccountMaxAttempts: maxFailedLoginsPerAccount,
		BruteforceAccountWindow:      10 * time.Minute,
		BruteforceAccountLockout:     time.Minute,

		ServiceName: "peekaping:apitest",
	}

	container := dig.New()
	require.NoError(t, wiring.RegisterSingle(container, cfg))
	require.NoError(t, container.Invoke(func(db *bun.DB, bus events.EventBus) error {
		t.Cleanup(func() {
			require.NoError(t, bus.Close(context.Background()))
			require.NoError(t, db.Close())
		})
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		if err := migrator.Init(ctx); err != nil {
			return err
		}
		_, err := migrator.Migrate(ctx)
		return err
	}))
	require.NoError(t, container.Invoke(func(settings setting.Service) error { return settings.InitializeSettings(ctx) }))
	require.NoError(t, container.Invoke(func(waker *protocol.Waker) { waker.Subscribe() }))

	a := &app{container: container}
	require.NoError(t, container.Invoke(func(server *internal.Server) { a.router = server.Router }))
	a.registerAdmin(t)
	return a
}

// inProcessTransport serves the requests of a probe with the app's router, without a network
type inProcessTransport struct {
	router http.Handler
}

func (rt inProcessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	served := httptest.NewRequest(req.Method, req.URL.String(), req.Body).WithContext(req.Context())
	served.Header = req.Header.Clone()
	served.ContentLength = req.ContentLength
	recorder := httptest.NewRecorder()
	rt.router.ServeHTTP(recorder, served)
	resp := recorder.Result()
	resp.Request = req
	return resp, nil
}

// startProbe runs a probe client of the app with the token, until the test ends, when it shuts down
// as the program would. change, if given, changes the probe's config before it is made.
func (a *app) startProbe(t *testing.T, token string, change ...func(*probeclient.Config)) *probeclient.Probe {
	t.Helper()
	cfg := probeclient.Config{
		URL:        "http://probe.test",
		Token:      token,
		Version:    "0.0.0-test",
		HTTPClient: &http.Client{Transport: inProcessTransport{router: a.router}},
		Logger:     zap.NewNop().Sugar(),
	}
	for _, apply := range change {
		apply(&cfg)
	}
	probe := probeclient.New(cfg)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, probe.Run(ctx))
	}()
	t.Cleanup(func() {
		stop()
		<-done
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		assert.NoError(t, probe.Shutdown(shutdown))
	})
	return probe
}

// checkTarget is a web server that monitors check, and notes when each request came
type checkTarget struct {
	*httptest.Server

	mu    sync.Mutex
	times map[string][]time.Time // by path
}

func newCheckTarget(t *testing.T) *checkTarget {
	t.Helper()
	target := &checkTarget{times: map[string][]time.Time{}}
	target.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target.mu.Lock()
		target.times[r.URL.Path] = append(target.times[r.URL.Path], time.Now())
		target.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	return target
}

// requests are the times the path was requested, in order
func (c *checkTarget) requests(path string) []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.times[path])
}

// monitorOfTarget is the body of an HTTP monitor that checks the target's path, on the probe
func monitorOfTarget(name, targetURL, probeID string) map[string]any {
	return onProbes(with(httpMonitorBody(name, ""), "config",
		fmt.Sprintf(`{"url":%q,"method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`, targetURL)), probeID)
}

// checkEverySecond sets the monitor's check interval to a second. The API takes 20 s as the least,
// which would make these tests wait for minutes; the row is changed under it.
func (a *app) checkEverySecond(t *testing.T, monitorID string) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.NewUpdate().Table("monitors").Set("interval = 1").Where("id = ?", monitorID).Exec(context.Background())
		return err
	}))
}

// heartbeatsOf are the monitor's heartbeats, oldest first
func (a *app) heartbeatsOf(t *testing.T, monitorID string) []*heartbeat.Model {
	t.Helper()
	var beats []*heartbeat.Model
	require.NoError(t, a.container.Invoke(func(svc heartbeat.Service) error {
		var err error
		beats, err = svc.FindByMonitorIDPaginated(context.Background(), monitorID, 1000, 0, nil, false)
		return err
	}))
	slices.Reverse(beats)
	return beats
}

func TestProbeClient_ChecksItsAssignmentAndTheHeartbeatsArrive(t *testing.T) {
	a := newSingleApp(t)
	target := newCheckTarget(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "eu-central"})
	monitorID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, monitorOfTarget("Checkout", target.URL+"/checkout", frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	a.checkEverySecond(t, monitorID)

	probe := a.startProbe(t, frankfurt.Token)

	require.Eventually(t, func() bool { return len(a.heartbeatsOf(t, monitorID)) >= 3 }, 20*time.Second, 50*time.Millisecond,
		"the probe checks the monitor at once and then on its interval, and the server stores the results")
	assert.True(t, probe.Ready())
	beats := a.heartbeatsOf(t, monitorID)
	requests := target.requests("/checkout")
	for i, beat := range beats {
		assert.Equal(t, shared.MonitorStatusUp, beat.Status, "heartbeat %d", i+1)
		assert.False(t, beat.EndTime.Before(beat.Time), "heartbeat %d ends after it starts", i+1)
		// The beat carries the times the probe's check ran, which is when the target was asked
		asked := slices.ContainsFunc(requests, func(at time.Time) bool {
			return !at.Before(beat.Time.Add(-5*time.Millisecond)) && !at.After(beat.EndTime.Add(5*time.Millisecond))
		})
		assert.True(t, asked, "heartbeat %d (%s to %s): the target was asked in between, at %v", i+1, beat.Time, beat.EndTime, requests)
	}
	for i := 1; i < len(beats); i++ {
		assert.True(t, beats[i].Time.After(beats[i-1].Time), "the heartbeats are in the order of their checks")
	}

	got := a.getProbe(t, frankfurt.ID)
	require.Len(t, got.Instances, 1)
	assert.Equal(t, probe.InstanceID(), got.Instances[0].ID, "the server knows the instance by the id the probe made")
	assert.Contains(t, got.SupportedTypes, "http")
	assert.NotContains(t, got.SupportedTypes, "push", "a probe never runs a push monitor")
}

func TestProbeClient_MonitorMovedAwayStops(t *testing.T) {
	a := newSingleApp(t)
	target := newCheckTarget(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	monitorID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, monitorOfTarget("Checkout", target.URL+"/checkout", frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	a.checkEverySecond(t, monitorID)
	a.startProbe(t, frankfurt.Token)
	require.Eventually(t, func() bool { return len(target.requests("/checkout")) >= 3 }, 20*time.Second, 20*time.Millisecond,
		"the probe checks the monitor every second")

	decode[monitorWithProbes](t, a.request(t, http.MethodPatch, monitorsPath+"/"+monitorID, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin), http.StatusOK)
	movedAt := time.Now()

	// The poll wakes at once and the probe takes the monitor off its schedule: it may finish a check
	// it had started, and then there are none
	time.Sleep(1500 * time.Millisecond)
	settled := len(target.requests("/checkout"))
	require.Never(t, func() bool { return len(target.requests("/checkout")) > settled }, 3500*time.Millisecond, 50*time.Millisecond,
		"the probe does not check a monitor that is not its own any more")
	for _, at := range target.requests("/checkout") {
		assert.True(t, at.Before(movedAt.Add(1500*time.Millisecond)), "no check after the monitor moved, but for one already running")
	}
}
