package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

// bootstrapToken is 43 characters
const bootstrapToken = "bootstrap-token-0123456789-abcdefghijklmnop"

// localProbe reads the local probe through the API
func (in *instance) localProbe(t *testing.T) (tokenPrefix string) {
	t.Helper()
	status, body := in.request(t, http.MethodGet, "/api/v1/probes/local", nil)
	return decode[struct {
		ID          string `json:"id"`
		TokenPrefix string `json:"token_prefix"`
	}](t, status, body, http.StatusOK).TokenPrefix
}

// singleModeRefusal is what a probe that uses the local probe's token is told
const singleModeRefusal = "this install runs in single mode, where the peekaping process checks the local probe's monitors; use a remote probe's token"

// AC5: single mode doesn't store the token, and says it ignores it, once
func TestRun_DoesNotStoreTheProbeBootstrapTokenAndWarnsOnce(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": bootstrapToken})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	assert.Equal(t, "", in.localProbe(t), "the local probe has no token")

	warned := logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All()
	require.Len(t, warned, 1)
	assert.Equal(t, zapcore.WarnLevel, warned[0].Level)
	assert.Equal(t, "PROBE_BOOTSTRAP_TOKEN is set, but single mode doesn't use it: the peekaping process checks the local probe's monitors and refuses a probe instance that uses the local probe's token. Give peekaping-probe a remote probe's token.", warned[0].Message)
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, bootstrapToken)
		for _, field := range entry.Context {
			assert.NotContains(t, field.String, bootstrapToken)
		}
	}
}

func TestRun_WithoutAProbeBootstrapTokenTheLocalProbeHasNoTokenAndNothingIsSaid(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": ""})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	assert.Equal(t, "", in.localProbe(t))
	assert.Empty(t, logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All())
}

// AC5: the token is not the local probe's, so a peekaping-probe that sends it is told it is unknown
func TestRun_DoesNotAcceptAProbeThatSendsTheIgnoredProbeBootstrapToken(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": bootstrapToken})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	client := probeproto.NewClient(in.url, bootstrapToken, "probe-instance-1", http.DefaultClient)

	_, err := client.Hello(context.Background(), &probeproto.HelloRequest{
		InstanceID: "probe-instance-1", ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"},
	})

	var refused *probeproto.Error
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, http.StatusUnauthorized, refused.Status)
	assert.Equal(t, "invalid probe token", refused.Message)
}

// remoteProbe creates a remote probe through the API and returns its id and token
func (in *instance) remoteProbe(t *testing.T) (id, token string) {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/probes", map[string]any{"name": "Frankfurt"})
	created := decode[struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}](t, status, body, http.StatusCreated)
	return created.ID, created.Token
}

// onProbe has the monitor run on the probe
func onProbe(monitor map[string]any, probeID string) map[string]any {
	monitor["probe_ids"] = []string{probeID}
	return monitor
}

// probeInstance says hello as an instance of the remote probe with the token, over the wire, and
// returns the client and the version of the monitors it holds
func (in *instance) probeInstance(t *testing.T, token, instanceID string) (*probeproto.Client, string) {
	t.Helper()
	client := probeproto.NewClient(in.url, token, instanceID, http.DefaultClient)
	_, err := client.Hello(context.Background(), &probeproto.HelloRequest{
		InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"},
	})
	require.NoError(t, err)
	held, err := client.Assignments(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, held)
	return client, held.Version
}

// pollAnswer is what a probe's assignment poll came back with
type pollAnswer struct {
	assignments *probeproto.AssignmentsResponse
	err         error
}

// poll has the probe poll in the background
func poll(client *probeproto.Client, version string) <-chan pollAnswer {
	answered := make(chan pollAnswer, 1)
	go func() {
		assignments, err := client.Assignments(context.Background(), version)
		answered <- pollAnswer{assignments, err}
	}()
	return answered
}

// A probe's poll is answered by an event, and in single mode events stay in the process
func TestRun_AnswersAProbesAssignmentPollWhenAMonitorIsCreatedWithoutRedis(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	probeID, token := in.remoteProbe(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer target.Close()
	client, version := in.probeInstance(t, token, "probe-instance-1")
	answered := poll(client, version)
	time.Sleep(300 * time.Millisecond) // the poll is held

	created := in.createMonitor(t, onProbe(httpMonitor("Checkout", target.URL, 3600), probeID))

	select {
	case got := <-answered:
		require.NoError(t, got.err)
		require.NotNil(t, got.assignments, "a poll that was answered by the monitor, not by its timeout")
		require.Len(t, got.assignments.Monitors, 1)
		assert.Equal(t, created, got.assignments.Monitors[0].ID)
		assert.NotEqual(t, version, got.assignments.Version)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the poll was not answered within 5 seconds of the monitor being created")
	}
}

// Shutdown waits for the requests in flight, and a poll is one that waits for 25 seconds
func TestRun_ShutsDownWithinSecondsWhileAProbeHoldsAnAssignmentPoll(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	_, token := in.remoteProbe(t)
	client, version := in.probeInstance(t, token, "probe-instance-1")
	answered := poll(client, version)
	time.Sleep(300 * time.Millisecond) // the poll is held

	startedAt := time.Now()
	require.NoError(t, in.stop(t), "run returns nil, so peekaping exits 0")
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	assert.Less(t, took, 5*time.Second, "the poll doesn't hold up the shutdown")
	select {
	case got := <-answered:
		require.NoError(t, got.err)
		assert.Nil(t, got.assignments, "the poll was answered with 304")
	case <-time.After(wait):
		assert.Fail(t, "the poll was not answered")
	}
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

// Single mode has no queue: the reply to a probe's results comes after they are stored as
// heartbeats, with the times of the checks and not of their arrival.
func TestRun_StoresTheResultsAProbeSendsAsHeartbeatsBeforeTheReplyWithoutRedis(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	probeID, token := in.remoteProbe(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer target.Close()
	monitorID := in.createMonitor(t, onProbe(httpMonitor("Checkout", target.URL, 3600), probeID))
	client, _ := in.probeInstance(t, token, "probe-instance-1")
	failedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Millisecond)
	recoveredAt := failedAt.Add(time.Minute)

	got, err := client.Results(context.Background(), &probeproto.ResultsRequest{Results: []probeproto.Result{
		{ID: "recovered", MonitorID: monitorID, Status: probeproto.StatusUp, Message: "probe: 200 OK", PingMs: 31,
			StartTime: recoveredAt, EndTime: recoveredAt.Add(31 * time.Millisecond)},
		{ID: "failed", MonitorID: monitorID, Status: probeproto.StatusDown, Message: "probe: connection refused", PingMs: 5,
			StartTime: failedAt, EndTime: failedAt.Add(5 * time.Millisecond)},
	}})

	require.NoError(t, err)
	assert.Equal(t, []probeproto.ResultOutcome{{ID: "recovered", Accepted: true}, {ID: "failed", Accepted: true}}, got.Results)
	byMessage := map[string]shared.HeartBeatModel{}
	for _, beat := range in.heartbeats(t, monitorID) {
		byMessage[beat.Msg] = beat
	}
	failed, recovered := byMessage["probe: connection refused"], byMessage["probe: 200 OK"]
	require.NotEmpty(t, failed.ID, "the heartbeat of the failed check is stored when the reply comes")
	require.NotEmpty(t, recovered.ID)
	assert.Equal(t, shared.MonitorStatusDown, failed.Status)
	assert.Equal(t, 5, failed.Ping)
	assert.True(t, failedAt.Equal(failed.Time), "the start of the check, not of its arrival: %s", failed.Time)
	assert.True(t, failedAt.Add(5*time.Millisecond).Equal(failed.EndTime), "%s", failed.EndTime)
	assert.Equal(t, shared.MonitorStatusUp, recovered.Status)
	assert.Equal(t, 31, recovered.Ping)
	assert.True(t, recoveredAt.Equal(recovered.Time), "%s", recovered.Time)
	assert.True(t, recoveredAt.Add(31*time.Millisecond).Equal(recovered.EndTime), "%s", recovered.EndTime)
}

// login logs the admin in, who registered before a restart, and sends the requests that follow
// with the access token
func (in *instance) login(t *testing.T) {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": adminEmail, "password": adminPassword,
	})
	in.accessToken = decode[struct {
		AccessToken string `json:"accessToken"`
	}](t, status, body, http.StatusOK).AccessToken
}
