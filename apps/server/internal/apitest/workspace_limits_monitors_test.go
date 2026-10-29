package apitest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/limits"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-160: a workspace's limits. On the cloud they are its free tier; on a self-hosted install there
// are none unless its admin sets them. The install's defaults are the WORKSPACE_LIMIT_* variables,
// and an instance admin can give a workspace limits of its own.

// withMonitorLimit sets WORKSPACE_LIMIT_MONITORS
func withMonitorLimit(n int) func(*config.Config) {
	return func(cfg *config.Config) { cfg.WorkspaceLimitMonitors = n }
}

// setOwnLimits gives the workspace limits of its own, as an instance admin will (nil removes them)
func (a *app) setOwnLimits(t *testing.T, id workspace.ID, own *limits.Limits) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(chain *limits.Chain) error {
		return chain.SetOwn(context.Background(), id, own)
	}))
}

func limitOf(n int) *int { return &n }

// limitedMonitor is the part of a monitor the limit tests read
type limitedMonitor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	OverLimit bool   `json:"over_limit"`
}

// limitReached is the 403 for a write over a limit
type limitReached struct {
	Message string `json:"message"`
	Data    any    `json:"data"`
	Error   struct {
		Code  string `json:"code"`
		Limit string `json:"limit"`
		Max   int    `json:"max"`
	} `json:"error"`
}

func assertLimitReached(t *testing.T, w *httpRecorder, limit string, max int) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var got limitReached
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	assert.Equal(t, "LIMIT_REACHED", got.Error.Code)
	assert.Equal(t, limit, got.Error.Limit)
	assert.Equal(t, max, got.Error.Max)
	assert.Nil(t, got.Data)
	assert.NotEmpty(t, got.Message)
}

func (a *app) limitedMonitors(t *testing.T) map[string]limitedMonitor {
	t.Helper()
	listed := decode[[]limitedMonitor](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	byID := make(map[string]limitedMonitor, len(listed))
	for _, m := range listed {
		byID[m.ID] = m
	}
	return byID
}

func (a *app) limitedMonitor(t *testing.T, id string) limitedMonitor {
	t.Helper()
	return decode[limitedMonitor](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK)
}

// running are the ids of the monitors that the lists of monitors to run hold: the scheduler's (by
// probe), the watchdog's (paged) and /metrics' (all at once), which must agree
func (a *app) running(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	byProbe := scheduled(t, a.schedulerSource(t))
	var paged, all []string
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		for page := 0; ; page++ {
			found, err := monitors.FindActivePaginatedAnyWorkspace(ctx, page, 2)
			if err != nil {
				return err
			}
			for _, m := range found {
				paged = append(paged, m.ID)
			}
			if len(found) < 2 {
				break
			}
		}
		found, err := monitors.FindActiveAnyWorkspace(ctx)
		for _, m := range found {
			all = append(all, m.ID)
		}
		return err
	}))
	assert.ElementsMatch(t, byProbe, paged, "the watchdog's pages hold what the scheduler runs")
	assert.ElementsMatch(t, byProbe, all, "and so does /metrics")
	return byProbe
}

// AC2: a create while the workspace has as many active monitors as its limit is refused, and one
// below it is not
func TestLimits_Monitors_ACreateAtTheLimitIsRefused(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withMonitorLimit(2))

		a.createHTTPMonitor(t, "One", "")
		a.createHTTPMonitor(t, "Two", "")
		w := a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Three", ""), a.asAdmin)

		assertLimitReached(t, w, "monitors", 2)
		assert.Contains(t, w.Body.String(), "2 active monitors", "the message names the limit's value")
		assert.Len(t, a.limitedMonitors(t), 2, "the refused create stored nothing")
	})
}

// AC2: a workspace above its limit (it was lowered) can't create one either
func TestLimits_Monitors_ACreateAboveTheLimitIsRefused(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createHTTPMonitor(t, "One", "")
	a.createHTTPMonitor(t, "Two", "")
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{Monitors: limitOf(1)})

	assertLimitReached(t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Three", ""), a.asAdmin), "monitors", 1)
}

// AC2: a paused monitor doesn't count, so it can be created at the limit; resuming it, by PATCH or
// PUT, is refused until another is paused
func TestLimits_Monitors_AResumeAtTheLimitIsRefusedAndPausingIsAllowed(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withMonitorLimit(1))
		running := a.createHTTPMonitor(t, "Running", "")
		paused := decode[limitedMonitor](t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("Paused", ""), "active", false), a.asAdmin), http.StatusCreated)
		path := monitorsPath + "/" + paused.ID

		assertLimitReached(t, a.request(t, http.MethodPatch, path, map[string]any{"active": true}, a.asAdmin), "monitors", 1)
		assertLimitReached(t, a.request(t, http.MethodPut, path, with(httpMonitorBody("Paused", ""), "active", true), a.asAdmin), "monitors", 1)
		assert.False(t, a.limitedMonitor(t, paused.ID).Active, "the refused resumes changed nothing")
		renamed := decode[limitedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Still paused"}, a.asAdmin), http.StatusOK)
		assert.Equal(t, "Still paused", renamed.Name, "a write that doesn't resume it is not refused")

		pausedNow := decode[limitedMonitor](t, a.request(t, http.MethodPatch, monitorsPath+"/"+running, map[string]any{"active": false}, a.asAdmin), http.StatusOK)
		assert.False(t, pausedNow.Active, "pausing is always allowed")
		resumed := decode[limitedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"active": true}, a.asAdmin), http.StatusOK)
		assert.True(t, resumed.Active, "with a place free it resumes")
	})
}

// AC2: lowering the limit pauses and deletes nothing. The newest active monitors past it are not
// run and say over_limit; the owner pauses others to choose which stay; raising it brings them back.
func TestLimits_Monitors_LoweringTheLimitLeavesEveryMonitorAndRunsTheOldest(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		oldest := a.createHTTPMonitor(t, "Oldest", "")
		middle := a.createHTTPMonitor(t, "Middle", "")
		newest := a.createHTTPMonitor(t, "Newest", "")
		push := decode[limitedMonitor](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Newest push", "limit-push-token"), a.asAdmin), http.StatusCreated).ID

		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{Monitors: limitOf(2)})

		listed := a.limitedMonitors(t)
		require.Len(t, listed, 4, "nothing is deleted")
		for _, id := range []string{oldest, middle, newest, push} {
			assert.True(t, listed[id].Active, "nothing is paused")
		}
		assert.False(t, listed[oldest].OverLimit)
		assert.False(t, listed[middle].OverLimit)
		assert.True(t, listed[newest].OverLimit, "the newest past the limit say so")
		assert.True(t, listed[push].OverLimit)
		assert.True(t, a.limitedMonitor(t, newest).OverLimit, "and so does the monitor's own read")
		assert.ElementsMatch(t, []string{oldest, middle}, a.running(t), "only the oldest two run")

		found, err := a.schedulerSource(t).Find(context.Background(), newest)
		require.NoError(t, err)
		assert.Nil(t, found, "the scheduler doesn't load one past the limit")
		require.NoError(t, a.container.Invoke(func(loader *check.Loader) {
			_, err := loader.Load(context.Background(), newest)
			assert.ErrorIs(t, err, check.ErrMonitorOverLimit, "nor does a check that was queued for it")
		}))
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodGet, "/api/v1/push/limit-push-token", nil).Code, "a push to one past the limit is refused")

		// The owner pauses the middle one, and the newest takes its place
		a.pauseMonitor(t, middle)
		assert.False(t, a.limitedMonitor(t, newest).OverLimit)
		assert.ElementsMatch(t, []string{oldest, newest}, a.running(t))

		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{Monitors: limitOf(3)})
		for id, m := range a.limitedMonitors(t) {
			assert.False(t, m.OverLimit, id)
		}
		assert.ElementsMatch(t, []string{oldest, newest, push}, a.running(t), "raising it brings them back")
	})
}

// AC5: two creates at once at one below the limit end with one of them refused
func TestLimits_Monitors_TwoCreatesAtOnceBelowTheLimitStoreOne(t *testing.T) {
	backends := map[string]func(*config.Config){"SQLite": func(*config.Config) {}}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends["PostgreSQL"] = usePostgres(t, dsn)
	}
	for name, backend := range backends {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, backend, withMonitorLimit(3))
			a.createHTTPMonitor(t, "One", "")
			a.createHTTPMonitor(t, "Two", "")

			const racers = 6
			codes := make([]int, racers)
			var wg sync.WaitGroup
			for i := range racers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					codes[i] = a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Racer", ""), a.asAdmin).Code
				}()
			}
			wg.Wait()

			created, refused := 0, 0
			for _, code := range codes {
				switch code {
				case http.StatusCreated:
					created++
				case http.StatusForbidden:
					refused++
				}
			}
			assert.Equal(t, 1, created, "%v", codes)
			assert.Equal(t, racers-1, refused, "%v", codes)
			assert.Len(t, a.limitedMonitors(t), 3)
		})
	}
}

// AC1: an install that sets nothing has no limits
func TestLimits_Monitors_WithNothingSetThereAreNoLimits(t *testing.T) {
	a := newAppWithAdmin(t)
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = a.createHTTPMonitor(t, "Site", "")
	}

	for id, m := range a.limitedMonitors(t) {
		assert.False(t, m.OverLimit, id)
	}
	assert.ElementsMatch(t, ids, a.running(t))
	var origin limits.Origin
	require.NoError(t, a.container.Invoke(func(source limits.Source) error {
		got, from, err := source.For(context.Background(), a.firstWorkspace(t))
		assert.True(t, got.IsZero())
		origin = from
		return err
	}))
	assert.Equal(t, limits.OriginNone, origin)
}

// AC3: the minimum check interval is the stricter of the install's and the workspace's, on save
// and when the monitor runs
func TestLimits_MinCheckInterval_TheWorkspacesLimitAppliesOnSaveAndAtRunTime(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))
	stored := a.storeMonitorAt(t, "Old", 30, 90)
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{MinCheckInterval: limitOf(120)})

	assertIntervalTooShortAt(t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("New", ""), "interval", 90), a.asAdmin), "interval", 120)
	assertIntervalTooShortAt(t, a.request(t, http.MethodPatch, monitorsPath+"/"+stored, map[string]any{"retry_interval": 90}, a.asAdmin), "retry_interval", 120)
	decode[limitedMonitor](t, a.request(t, http.MethodPost, monitorsPath, with(with(httpMonitorBody("New", ""), "interval", 120), "retry_interval", 120), a.asAdmin), http.StatusCreated)

	found, err := a.schedulerSource(t).Find(context.Background(), stored)
	require.NoError(t, err)
	assert.Equal(t, 120, found.Interval, "the checks run at the workspace's minimum")
	assert.Equal(t, 120, found.RetryInterval)
	listed, err := a.schedulerSource(t).ListActive(context.Background(), 0, 100)
	require.NoError(t, err)
	for _, m := range listed {
		assert.GreaterOrEqual(t, m.Interval, 120, m.Name)
	}

	// The install's minimum still holds where it is the stricter one
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{MinCheckInterval: limitOf(30)})
	assertIntervalTooShortAt(t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("New", ""), "interval", 45), a.asAdmin), "interval", 60)
	found, err = a.schedulerSource(t).Find(context.Background(), stored)
	require.NoError(t, err)
	assert.Equal(t, 60, found.Interval)
}

func assertIntervalTooShortAt(t *testing.T, w *httpRecorder, field string, minimum int) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var got intervalTooShortError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	assert.Equal(t, "INTERVAL_TOO_SHORT", got.Error.Code)
	assert.Equal(t, field, got.Error.Field)
	assert.Equal(t, minimum, got.Error.Minimum)
}

// AC4: a monitor runs on at most the workspace's number of probes
func TestLimits_Probes_MoreProbeIDsThanTheLimitAreRefused(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	paris := a.probeThatRuns(t, "Paris", "http")
	site := a.createHTTPMonitor(t, "Site", "")
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{ProbesPerMonitor: limitOf(2)})

	assertLimitReached(t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Everywhere", ""), "local", frankfurt.ID, paris.ID), a.asAdmin), "probes", 2)
	assertLimitReached(t, a.request(t, http.MethodPut, monitorsPath+"/"+site, onProbes(httpMonitorBody("Site", ""), "local", frankfurt.ID, paris.ID), a.asAdmin), "probes", 2)
	assertLimitReached(t, a.request(t, http.MethodPatch, monitorsPath+"/"+site, map[string]any{"probe_ids": []string{"local", frankfurt.ID, paris.ID}}, a.asAdmin), "probes", 2)
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, site), "the refused updates changed nothing")

	two := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Two", ""), "local", paris.ID), a.asAdmin), http.StatusCreated)
	assert.ElementsMatch(t, []string{"local", paris.ID}, two.ProbeIDs, "the limit itself saves")
}

// AC4: a monitor created without probes runs on the first default probes, as many as the limit
func TestLimits_Probes_TheDefaultProbesAreCutToTheLimit(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	paris := a.probeThatRuns(t, "Paris", "http")
	a.makeDefault(t, frankfurt.ID, true)
	a.makeDefault(t, paris.ID, true)
	all := a.createdOn(t, httpMonitorBody("All defaults", ""))
	require.Len(t, all, 3, "without a limit it runs on every default")

	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{ProbesPerMonitor: limitOf(2)})
	cut := a.createdOn(t, httpMonitorBody("Cut", ""))

	require.Len(t, cut, 2)
	assert.Contains(t, cut, "local", "the defaults keep their order, the local probe first")
}

// AC4: assigning a probe to every monitor skips a monitor that runs on as many probes as its
// workspace allows
func TestLimits_Probes_AssignAllSkipsAMonitorAtItsLimit(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	paris := a.probeThatRuns(t, "Paris", "http")
	one := a.createHTTPMonitor(t, "On one probe", "")
	two := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("On two", ""), "local", frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{ProbesPerMonitor: limitOf(2)})

	result := decode[assignAllResult](t, a.assignAllMonitors(t, paris.ID), http.StatusOK)

	assert.Equal(t, 1, result.Added)
	assert.ElementsMatch(t, []string{"local", paris.ID}, a.monitorProbes(t, one))
	assert.ElementsMatch(t, []string{"local", frankfurt.ID}, a.monitorProbes(t, two), "the monitor at its limit is skipped")
}

// The monitor service's own errors stay what they were: a write over a limit is a limits error
func TestLimits_TheRefusalIsALimitsError(t *testing.T) {
	a := newAppWithAdmin(t, withMonitorLimit(1))
	a.createHTTPMonitor(t, "One", "")
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) {
		_, err := monitors.Create(context.Background(), a.firstWorkspace(t), &monitor.CreateUpdateDto{
			Type: "http", Name: "Two", Interval: 60, Timeout: 16, RetryInterval: 60, Active: true,
			Config: httpMonitorBody("Two", "")["config"].(string),
		})
		var reached *limits.ReachedError
		require.True(t, errors.As(err, &reached), "%v", err)
		assert.Equal(t, limits.Monitors, reached.Limit)
		assert.Equal(t, 1, reached.Max)
	}))
}
