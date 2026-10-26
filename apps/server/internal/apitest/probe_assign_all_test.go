package apitest

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// monitorsOnProbe are the ids of the monitors that have a row for the probe, read from the table
// and not through the API, which shows a monitor without rows on the local probe
func (a *app) monitorsOnProbe(t *testing.T, probeID string) []string {
	t.Helper()
	ids := []string{}
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("monitor_probes").Column("monitor_id").Where("probe_id = ?", probeID).Scan(context.Background(), &ids)
	}))
	return ids
}

// AC1: a probe made with assign_all_monitors waits to be assigned and has no monitors yet. One made
// without the field is as it was.
func TestProbeAssignAll_CreateMarksTheProbeAsWaiting(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createHTTPMonitor(t, "Checkout", "")

	waiting := a.createProbe(t, map[string]any{"name": "Frankfurt", "assign_all_monitors": true})
	assert.True(t, waiting.AssignAllPending)
	assert.True(t, a.getProbe(t, waiting.ID).AssignAllPending, "a read of the probe shows it too")
	assert.Empty(t, a.monitorsOnProbe(t, waiting.ID), "the probe gets its monitors on its first hello, not at its creation")

	for name, body := range map[string]map[string]any{
		"without the field": {"name": "Paris"},
		"set to false":      {"name": "Lyon", "assign_all_monitors": false},
	} {
		t.Run(name, func(t *testing.T) {
			plain := a.createProbe(t, body)
			assert.False(t, plain.AssignAllPending)
			assert.Empty(t, a.monitorsOnProbe(t, plain.ID))
		})
	}
	assert.False(t, a.getProbe(t, "local").AssignAllPending)
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin).Code)
}

// optionalTypes are the monitor types a slim build of the probe leaves out
var optionalTypes = []string{"docker", "grpc-keyword", "kafka-producer", "mongodb", "sqlserver"}

// insertMonitor stores a monitor of any type directly, with no row in monitor_probes, which is how a
// monitor that predates the probes is stored. It skips the type's config checks, which a monitor of
// a type that needs a service to talk to would fail.
func (a *app) insertMonitor(t *testing.T, monitorType, name string, active bool) string {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active, config)
			 VALUES (?, ?, ?, 60, 16, 0, 60, 0, ?, '{}')`, id, monitorType, name, active)
		return err
	}))
	return id
}

// A world to assign: monitors of several types on the local probe, one paused, one push, and one
// that is on the waiting probe already
type assignWorld struct {
	probe   probeWithToken
	http    string
	paused  string
	tcp     string
	push    string
	already string
	// byType are the monitors of the types a slim build leaves out
	byType map[string]string
}

func (a *app) assignWorld(t *testing.T) assignWorld {
	t.Helper()
	w := assignWorld{byType: map[string]string{}}
	w.probe = a.createProbe(t, map[string]any{"name": "Frankfurt", "assign_all_monitors": true})
	w.http = a.createHTTPMonitor(t, "Site", "")
	w.paused = a.createHTTPMonitor(t, "Paused site", "")
	a.pauseMonitor(t, w.paused)
	w.tcp = decode[created](t, a.request(t, http.MethodPost, monitorsPath, tcpMonitorBody("Database"), a.asAdmin), http.StatusCreated).ID
	w.push = a.createMonitor(t, "Nightly job", "nightly-token")
	w.already = decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Both", ""), "local", w.probe.ID), a.asAdmin), http.StatusCreated).ID
	for _, monitorType := range optionalTypes {
		w.byType[monitorType] = a.insertMonitor(t, monitorType, monitorType+" monitor", true)
	}
	return w
}

// every monitor that isn't a push monitor
func (w assignWorld) nonPush() []string {
	ids := []string{w.http, w.paused, w.tcp, w.already}
	for _, id := range w.byType {
		ids = append(ids, id)
	}
	return ids
}

// AC2, AC3: the first hello of a waiting probe assigns it every monitor that isn't a push monitor,
// paused ones included, of a type the instance runs. An instance that lists no types runs them all.
func TestProbeAssignAll_FirstHelloAssignsTheMonitorsTheInstanceRuns(t *testing.T) {
	for name, types := range map[string][]string{
		"an instance that lists no types": nil,
		"a full build":                    append([]string{"http", "tcp", "push"}, optionalTypes...),
	} {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			w := a.assignWorld(t)

			a.sayHelloWith(t, w.probe.Token, "instance-1", types)

			assert.ElementsMatch(t, w.nonPush(), a.monitorsOnProbe(t, w.probe.ID), "every monitor but the push one, the paused one and the one that was there already included")
			assert.False(t, a.getProbe(t, w.probe.ID).AssignAllPending, "the flag is cleared")
			assert.ElementsMatch(t, []string{"local", w.probe.ID}, a.monitorProbes(t, w.http), "a monitor keeps the probes it had")
			assert.Equal(t, []string{"local"}, a.monitorProbes(t, w.push), "a push monitor runs on the local probe only")
		})
	}
}

// AC2: a slim build leaves out the Docker, gRPC keyword, Kafka producer, MongoDB and SQL Server
// monitors, which the probe can't run
func TestProbeAssignAll_ASlimInstanceGetsNoMonitorOfATypeItLeavesOut(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.assignWorld(t)

	a.sayHelloWith(t, w.probe.Token, "instance-slim", []string{"dns", "http", "tcp"})

	assert.ElementsMatch(t, []string{w.http, w.paused, w.tcp, w.already}, a.monitorsOnProbe(t, w.probe.ID))
	for monitorType, id := range w.byType {
		assert.Equal(t, []string{"local"}, a.monitorProbes(t, id), "a %s monitor stays where it was", monitorType)
	}
}

// AC3: the flag clears with the assignment, so a reconnect or a second instance assigns nothing
func TestProbeAssignAll_OnlyTheFirstHelloAssigns(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.assignWorld(t)
	a.sayHelloWith(t, w.probe.Token, "instance-1", nil)
	later := a.createHTTPMonitor(t, "Later", "")

	a.sayHelloWith(t, w.probe.Token, "instance-1", nil)
	assert.NotContains(t, a.monitorsOnProbe(t, w.probe.ID), later, "a reconnect of the instance")

	a.sayHelloWith(t, w.probe.Token, "instance-2", nil)
	assert.NotContains(t, a.monitorsOnProbe(t, w.probe.ID), later, "a second instance")
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, later))
}

// A probe made without the field is not assigned anything by its hello
func TestProbeAssignAll_AProbeMadeWithoutTheFieldGetsNothing(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createHTTPMonitor(t, "Site", "")
	plain := a.createProbe(t, map[string]any{"name": "Paris"})

	a.sayHelloWith(t, plain.Token, "instance-1", nil)

	assert.Empty(t, a.monitorsOnProbe(t, plain.ID))
}

// AC4: each changed monitor keeps its location rule and gets a new updated_at, and a monitor with no
// rows in monitor_probes keeps running on the local probe
func TestProbeAssignAll_ChangedMonitorsKeepTheirRuleAndGetANewUpdatedAt(t *testing.T) {
	a := newAppWithAdmin(t)
	other := a.createProbe(t, map[string]any{"name": "Tokyo"})
	waiting := a.createProbe(t, map[string]any{"name": "Frankfurt", "assign_all_monitors": true})
	all := decode[created](t, a.request(t, http.MethodPost, monitorsPath, withRule(onProbes(httpMonitorBody("All", ""), "local", other.ID), "all", nil), a.asAdmin), http.StatusCreated).ID
	atLeast := decode[created](t, a.request(t, http.MethodPost, monitorsPath, withRule(onProbes(httpMonitorBody("At least", ""), "local", other.ID), "at_least", ptr(2)), a.asAdmin), http.StatusCreated).ID
	push := a.createMonitor(t, "Nightly job", "nightly-token")
	bare := a.insertMonitor(t, "http", "Bare", true)
	before := a.storedMonitors(t)

	a.sayHelloWith(t, waiting.Token, "instance-1", nil)

	assert.ElementsMatch(t, []string{all, atLeast, bare}, a.monitorsOnProbe(t, waiting.ID))
	after := a.storedMonitors(t)
	for _, id := range []string{all, atLeast, bare} {
		assert.True(t, after[id].UpdatedAt.After(before[id].UpdatedAt), "updated_at of %s: %v then %v", id, before[id].UpdatedAt, after[id].UpdatedAt)
	}
	assert.True(t, after[push].UpdatedAt.Equal(before[push].UpdatedAt), "a monitor that isn't changed keeps its updated_at")

	allRule, atLeastRule := a.ruleOf(t, all), a.ruleOf(t, atLeast)
	assert.Equal(t, "all", allRule.LocationRule)
	assert.Nil(t, allRule.LocationThreshold)
	assert.Equal(t, "at_least", atLeastRule.LocationRule)
	assert.Equal(t, ptr(2), atLeastRule.LocationThreshold)
	assert.ElementsMatch(t, []string{"local", other.ID, waiting.ID}, a.monitorProbes(t, atLeast))
	assert.ElementsMatch(t, []string{"local", waiting.ID}, a.monitorProbes(t, bare), "the monitor that had no rows runs on the local probe still")
	assert.Contains(t, a.monitorsOnProbe(t, "local"), bare)
}

// AC4: each changed monitor is announced as updated, once the assignment is stored
func TestProbeAssignAll_PublishesMonitorUpdatedForEachChangedMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	waiting := a.createProbe(t, map[string]any{"name": "Frankfurt", "assign_all_monitors": true})
	a.createHTTPMonitor(t, "Site", "")
	a.createHTTPMonitor(t, "Other site", "")
	a.createMonitor(t, "Nightly job", "nightly-token")
	updated := a.watchMonitors(t, "assign-all", events.MonitorUpdated)

	a.sayHelloWith(t, waiting.Token, "instance-1", nil)

	updated.waitForMonitor(t, "Site")
	updated.waitForMonitor(t, "Other site")
	updated.mu.Lock()
	defer updated.mu.Unlock()
	require.Len(t, updated.seen, 2, "the push monitor is not announced")
	for _, m := range updated.seen {
		assert.Contains(t, m.ProbeIDs, waiting.ID, "the event has the probe the monitor was assigned")
	}
}

// failingLookup is the probe lookup of the monitor service, which fails while broken is set. The
// assignment looks the probe up after it claims the flag, so it fails with the claim made.
type failingLookup struct {
	monitor.ProbeLookup
	broken atomic.Bool
}

func (l *failingLookup) FindProbe(ctx context.Context, id string) (*monitor.ProbeInfo, error) {
	if l.broken.Load() {
		return nil, errors.New("the probe lookup is down")
	}
	return l.ProbeLookup.FindProbe(ctx, id)
}

// AC3: when the assignment fails the hello answers 503 and the flag stays, with nothing assigned,
// so that the probe's retry assigns
func TestProbeAssignAll_AFailedAssignmentAnswers503AndTheRetryAssigns(t *testing.T) {
	lookup := &failingLookup{}
	a := newAppDecorated(t, func(c *dig.Container) error {
		return c.Decorate(func(probes monitor.ProbeLookup) monitor.ProbeLookup {
			lookup.ProbeLookup = probes
			return lookup
		})
	})
	a.registerAdmin(t)
	w := a.assignWorld(t)
	hello := helloFrom("instance-1")
	hello.SupportedTypes = nil
	lookup.broken.Store(true)

	failed := a.request(t, http.MethodPost, helloPath, hello, asProbe(w.probe.Token, "instance-1"))

	assert.Equal(t, http.StatusServiceUnavailable, failed.Code, failed.Body.String())
	assert.True(t, a.getProbe(t, w.probe.ID).AssignAllPending, "the flag stays")
	assert.Equal(t, []string{w.already}, a.monitorsOnProbe(t, w.probe.ID), "and nothing is assigned")

	lookup.broken.Store(false)
	retried := a.request(t, http.MethodPost, helloPath, hello, asProbe(w.probe.Token, "instance-1"))

	require.Equal(t, http.StatusOK, retried.Code, retried.Body.String())
	assert.ElementsMatch(t, w.nonPush(), a.monitorsOnProbe(t, w.probe.ID))
	assert.False(t, a.getProbe(t, w.probe.ID).AssignAllPending)
}
