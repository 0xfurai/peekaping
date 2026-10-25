package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	// localProbeToken is 43 characters, as PROBE_BOOTSTRAP_TOKEN would be
	localProbeToken = "local-probe-token-0123456789-abcdefghijklmn"
	// singleModeRefusal is what a probe that uses the local probe's token is told in single mode
	singleModeRefusal = "this install runs in single mode, where the peekaping process checks the local probe's monitors; use a remote probe's token"
	probeInstance     = "probe-instance-1"
)

// probeInstall is an install of the graph under test, migrated, with the local probe's token
// stored, as an earlier run of the cluster would have left it
type probeInstall struct {
	container *dig.Container
	router    http.Handler
	probes    probe.Service
	monitors  monitor.Service
}

func newProbeInstall(t *testing.T, register func(*dig.Container) error) *probeInstall {
	t.Helper()
	container := dig.New()
	require.NoError(t, register(container))
	in := &probeInstall{container: container}
	require.NoError(t, container.Invoke(func(
		probes probe.Service, monitors monitor.Service, settings setting.Service, server *internal.Server, bus events.EventBus, db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))
		ctx := context.Background()
		require.NoError(t, settings.InitializeSettings(ctx))
		require.NoError(t, probes.Bootstrap(ctx, localProbeToken))
		in.probes, in.monitors, in.router = probes, monitors, server.Router
	}))
	return in
}

func newSingleInstall(t *testing.T) *probeInstall {
	return newProbeInstall(t, func(c *dig.Container) error { return RegisterSingle(c, singleConfig(t)) })
}

func newClusterInstall(t *testing.T) *probeInstall {
	return newProbeInstall(t, func(c *dig.Container) error { return RegisterClusterAPI(c, clusterConfig(t)) })
}

// serve serves the install's router on a port, for a client that dials it
func (in *probeInstall) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(in.router)
	t.Cleanup(server.Close)
	return server.URL
}

// client is a probe protocol client of the install with the token
func (in *probeInstall) client(t *testing.T, token string) *probeproto.Client {
	return probeproto.NewClient(in.serve(t), token, probeInstance, http.DefaultClient)
}

func hello() *probeproto.HelloRequest {
	return &probeproto.HelloRequest{
		InstanceID: probeInstance, ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"},
	}
}

// instancesOf are the instances of the probe that said hello
func (in *probeInstall) instancesOf(t *testing.T, probeID string) []*probe.Instance {
	t.Helper()
	found, err := in.probes.FindByID(context.Background(), probeID)
	require.NoError(t, err)
	require.NotNil(t, found)
	return found.Instances
}

// createMonitorOn creates an active HTTP monitor on the probe
func (in *probeInstall) createMonitorOn(t *testing.T, name, probeID string) string {
	t.Helper()
	created, err := in.monitors.Create(context.Background(), &monitor.CreateUpdateDto{
		Type: "http", Name: name, Active: true, Interval: 60, Timeout: 16, RetryInterval: 60, NotificationIds: []string{},
		Config:   `{"url":"http://127.0.0.1:9/","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`,
		ProbeIDs: []string{probeID},
	})
	require.NoError(t, err)
	return created.ID
}

// refusedWith reports that err is the server's 401 with the single mode message
func refusedWith(t *testing.T, err error) {
	t.Helper()
	var failure *probeproto.Error
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, http.StatusUnauthorized, failure.Status)
	assert.Equal(t, singleModeRefusal, failure.Message)
}

// AC1
func TestRegisterSingle_RefusesTheLocalProbesTokenOnEveryProbeCallAndRecordsNoInstance(t *testing.T) {
	in := newSingleInstall(t)
	in.createMonitorOn(t, "Home", probe.LocalID)
	client := in.client(t, localProbeToken)
	ctx := context.Background()

	_, err := client.Hello(ctx, hello())
	refusedWith(t, err)
	_, err = client.Assignments(ctx, "")
	refusedWith(t, err)
	_, err = client.Results(ctx, &probeproto.ResultsRequest{})
	refusedWith(t, err)
	refusedWith(t, client.Bye(ctx))

	assert.Empty(t, in.instancesOf(t, probe.LocalID), "no instance of the local probe is recorded")
}

// AC1: the instance header isn't read first, so a call without one is refused for the token
func TestRegisterSingle_RefusesTheLocalProbesTokenWithoutAnInstanceHeader(t *testing.T) {
	in := newSingleInstall(t)
	req := httptest.NewRequest(http.MethodPost, probeproto.PathPrefix+"/hello", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+localProbeToken)
	w := httptest.NewRecorder()

	in.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), singleModeRefusal)
}

// AC3
func TestRegisterSingle_ARemoteProbesTokenStillConnectsAndGetsItsMonitors(t *testing.T) {
	in := newSingleInstall(t)
	remote, err := in.probes.Create(context.Background(), &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)
	monitorID := in.createMonitorOn(t, "Checkout", remote.ID)
	client := in.client(t, remote.Token)

	welcome, err := client.Hello(context.Background(), hello())
	require.NoError(t, err)
	held, err := client.Assignments(context.Background(), "")
	require.NoError(t, err)

	assert.Equal(t, remote.ID, welcome.Probe.ID)
	require.NotNil(t, held)
	require.Len(t, held.Monitors, 1)
	assert.Equal(t, monitorID, held.Monitors[0].ID)
	require.Len(t, in.instancesOf(t, remote.ID), 1)
}

// AC4
func TestRegisterClusterAPI_TheLocalProbesTokenStillConnectsItsInstances(t *testing.T) {
	in := newClusterInstall(t)
	monitorID := in.createMonitorOn(t, "Home", probe.LocalID)
	client := in.client(t, localProbeToken)

	welcome, err := client.Hello(context.Background(), hello())
	require.NoError(t, err)
	held, err := client.Assignments(context.Background(), "")
	require.NoError(t, err)

	assert.Equal(t, probe.LocalID, welcome.Probe.ID)
	require.NotNil(t, held)
	require.Len(t, held.Monitors, 1)
	assert.Equal(t, monitorID, held.Monitors[0].ID)
	require.Len(t, in.instancesOf(t, probe.LocalID), 1)
}

// AC2: a peekaping-probe that the server refuses says why in its log
func TestRegisterSingle_APeekapingProbeWithTheLocalProbesTokenLogsTheRefusal(t *testing.T) {
	in := newSingleInstall(t)
	core, logs := observer.New(zap.ErrorLevel)
	cfg := probeclient.Config{
		URL: in.serve(t), Token: localProbeToken, Version: "0.0.0-test",
		HTTPClient: http.DefaultClient, Logger: zap.New(core).Sugar(),
	}
	probe := probeclient.New(cfg)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = probe.Run(ctx)
	}()
	t.Cleanup(func() { stop(); <-done })

	require.Eventually(t, func() bool { return logs.Len() > 0 }, 10*time.Second, 20*time.Millisecond, "the probe logs the refusal")

	entry := logs.All()[0]
	assert.Equal(t, "The server refused the probe token", entry.Message)
	assert.Equal(t, singleModeRefusal, entry.ContextMap()["message"])
	assert.NotContains(t, entry.ContextMap()["message"], localProbeToken)
}
