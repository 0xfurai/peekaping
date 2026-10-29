package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-159: MIN_CHECK_INTERVAL is the smallest interval and retry interval a monitor can be saved
// with. A monitor stored with less is checked at the minimum.

// withMinCheckInterval sets MIN_CHECK_INTERVAL, in seconds
func withMinCheckInterval(seconds int) func(*config.Config) {
	return func(cfg *config.Config) { cfg.MinCheckInterval = seconds }
}

// intervalTooShortError is the 400 for an interval or retry interval below the minimum
type intervalTooShortError struct {
	Message string `json:"message"`
	Error   struct {
		Code    string `json:"code"`
		Field   string `json:"field"`
		Minimum int    `json:"minimum"`
	} `json:"error"`
}

func assertIntervalTooShort(t *testing.T, w *httpRecorder, field string, minimum int) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var got intervalTooShortError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	assert.Equal(t, "INTERVAL_TOO_SHORT", got.Error.Code)
	assert.Equal(t, field, got.Error.Field)
	assert.Equal(t, minimum, got.Error.Minimum)
	assert.Contains(t, got.Message, "60", "the message names the minimum")
	assert.Contains(t, got.Message, field)
}

// storeMonitorAt writes an HTTP monitor with the intervals straight into the workspace, as one stored
// before the minimum was raised is
func (a *app) storeMonitorAt(t *testing.T, name string, interval, retryInterval int) string {
	t.Helper()
	var id string
	require.NoError(t, a.container.Invoke(func(repo monitor.MonitorRepository) error {
		stored, err := repo.Create(context.Background(), a.firstWorkspace(t), &shared.Monitor{
			Type: "http", Name: name, Interval: interval, Timeout: 16, RetryInterval: retryInterval, ResendInterval: 10,
			Active: true, Status: shared.MonitorStatusPending,
			Config: httpMonitorBody(name, "")["config"].(string),
		})
		if err != nil {
			return err
		}
		id = stored.ID
		return nil
	}))
	return id
}

func intervalsOf(t *testing.T, a *app, id string) (interval, retryInterval int) {
	t.Helper()
	got := decode[struct {
		Interval      int `json:"interval"`
		RetryInterval int `json:"retry_interval"`
	}](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK)
	return got.Interval, got.RetryInterval
}

// AC3: a save below the minimum is refused, by create, PUT and PATCH, for the interval and for the
// retry interval alike
func TestMinCheckInterval_ASaveBelowTheMinimumIsRefused(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))
	id := a.createHTTPMonitor(t, "Site", "")
	path := monitorsPath + "/" + id

	for _, field := range []string{"interval", "retry_interval"} {
		t.Run(field, func(t *testing.T) {
			assertIntervalTooShort(t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("Other", ""), field, 30), a.asAdmin), field, 60)
			assertIntervalTooShort(t, a.request(t, http.MethodPut, path, with(httpMonitorBody("Site", ""), field, 30), a.asAdmin), field, 60)
			assertIntervalTooShort(t, a.request(t, http.MethodPatch, path, map[string]any{field: 30}, a.asAdmin), field, 60)
		})
	}

	listed := decode[[]typedMonitor](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	assert.Len(t, listed, 1, "the refused create stored nothing")
	interval, retry := intervalsOf(t, a, id)
	assert.Equal(t, 60, interval, "and the refused updates changed nothing")
	assert.Equal(t, 60, retry)
}

func TestMinCheckInterval_TheMinimumItselfSaves(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))
	id := a.createHTTPMonitor(t, "Site", "")
	path := monitorsPath + "/" + id

	decode[typedMonitor](t, a.request(t, http.MethodPost, monitorsPath, with(with(httpMonitorBody("Other", ""), "interval", 60), "retry_interval", 60), a.asAdmin), http.StatusCreated)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPut, path, with(httpMonitorBody("Site", ""), "interval", 60), a.asAdmin).Code)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPatch, path, map[string]any{"interval": 90, "retry_interval": 60}, a.asAdmin).Code)
}

// A monitor stored below the minimum can still be renamed or paused: only a write that sets the
// interval or the retry interval is checked
func TestMinCheckInterval_APatchThatLeavesTheIntervalsAloneWorksOnAMonitorStoredBelowTheMinimum(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))
	id := a.storeMonitorAt(t, "Old", 30, 30)
	path := monitorsPath + "/" + id

	renamed := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Renamed", renamed.Name)
	paused := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"active": false}, a.asAdmin), http.StatusOK)
	assert.False(t, paused.Active)

	interval, retry := intervalsOf(t, a, id)
	assert.Equal(t, 30, interval, "the stored values are left as they are")
	assert.Equal(t, 30, retry)
	assertIntervalTooShort(t, a.request(t, http.MethodPatch, path, map[string]any{"interval": 30}, a.asAdmin), "interval", 60)
}

// AC3: a monitor stored below the minimum is checked at the minimum: the scheduler's source, the
// check job and the probe's assignment all carry it
func TestMinCheckInterval_AMonitorStoredBelowTheMinimumIsCheckedAtTheMinimum(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60), func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })
	id := a.storeMonitorAt(t, "Old", 30, 40)
	longID := a.storeMonitorAt(t, "Long", 300, 120)
	source := a.schedulerSource(t)

	listed, err := source.ListActive(context.Background(), 0, 1000)
	require.NoError(t, err)
	byID := map[string]*shared.Monitor{}
	for _, m := range listed {
		byID[m.ID] = m
	}
	assert.Equal(t, 60, byID[id].Interval, "the scheduler runs it every 60 s")
	assert.Equal(t, 60, byID[id].RetryInterval)
	assert.Equal(t, 16, byID[id].Timeout, "the timeout stays as stored")
	assert.Equal(t, 300, byID[longID].Interval, "an interval above the minimum is left alone")
	assert.Equal(t, 120, byID[longID].RetryInterval)
	found, err := source.Find(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, 60, found.Interval)
	assert.Equal(t, 60, found.RetryInterval)

	require.NoError(t, a.container.Invoke(func(loader *check.Loader) error {
		job, err := loader.Load(context.Background(), id)
		if err != nil {
			return err
		}
		assert.Equal(t, 60, job.Monitor.Interval)
		assert.Equal(t, 60, job.Monitor.RetryInterval)
		return nil
	}))

	a.sayHelloWith(t, localToken, "00000000-0000-4000-8000-000000000001", fullBuildTypes)
	dealt := decodeAssignments(t, a.assignments(t, localToken, "00000000-0000-4000-8000-000000000001", ""))
	require.Len(t, dealt.Monitors, 2)
	for _, m := range dealt.Monitors {
		if m.ID == id {
			assert.Equal(t, 60, m.Interval, "the probe is told 60")
			assert.Equal(t, 60, m.RetryInterval)
			assert.Equal(t, 16, m.Timeout)
		}
	}

	// the API still shows what is stored
	interval, retry := intervalsOf(t, a, id)
	assert.Equal(t, 30, interval)
	assert.Equal(t, 40, retry)
}

// A probe that holds an assignment from before the minimum was raised is dealt the new one: the
// version covers the interval the probe is told. The install restarts on the same database, with
// the minimum kept and then raised, so that the one monitor and the one probe instance stay the same.
func TestMinCheckInterval_TheAssignmentVersionFollowsTheMinimum(t *testing.T) {
	const instanceID = "00000000-0000-4000-8000-000000000001"
	probeToken := func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken }
	dealtBy := func(a *app) probeproto.AssignmentsResponse {
		t.Helper()
		a.sayHelloWith(t, localToken, instanceID, fullBuildTypes)
		dealt := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))
		require.Len(t, dealt.Monitors, 1)
		return dealt
	}

	low := newAppWithAdmin(t, withMinCheckInterval(20), probeToken)
	id := low.storeMonitorAt(t, "Old", 30, 30)
	before := dealtBy(low)
	assert.Equal(t, 30, before.Monitors[0].Interval, "a minimum of 20 leaves 30 alone")

	same := dealtBy(newReplica(t, low, withMinCheckInterval(20), probeToken))
	assert.Equal(t, before.Version, same.Version, "a restart with the same minimum deals the same assignment")

	raised := dealtBy(newReplica(t, low, withMinCheckInterval(60), probeToken))
	assert.Equal(t, id, raised.Monitors[0].ID)
	assert.Equal(t, 60, raised.Monitors[0].Interval)
	assert.NotEqual(t, before.Version, raised.Version, "the raised minimum changes what the probe is told")
}

// AC4: /me says the minimum
func TestMinCheckInterval_MeReturnsTheMinimum(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))

	assert.Equal(t, 60, a.meOf(t, a.asAdmin).MinCheckInterval)
}

// AC5: with nothing set the minimum is 20, which saves and which /me says
func TestMinCheckInterval_WithNothingSetTheMinimumIsTwenty(t *testing.T) {
	a := newAppWithAdmin(t)
	body := with(with(httpMonitorBody("Fast", ""), "interval", 20), "retry_interval", 20)

	made := decode[typedMonitor](t, a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin), http.StatusCreated)

	interval, retry := intervalsOf(t, a, made.ID)
	assert.Equal(t, 20, interval)
	assert.Equal(t, 20, retry)
	assert.Equal(t, 20, a.meOf(t, a.asAdmin).MinCheckInterval)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPatch, monitorsPath+"/"+made.ID, map[string]any{"interval": 25}, a.asAdmin).Code)
}
