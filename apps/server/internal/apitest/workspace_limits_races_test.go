package apitest

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/limits"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-160: a write that can go over a limit holds its workspace (limits.LockWorkspace) before it
// reads what it counts, and until it commits. These tests race such writes, and need PostgreSQL to
// mean anything: SQLite has one writer at a time anyway.

// racingBackends are SQLite and, when PEE_TEST_POSTGRES_DSN names a server, PostgreSQL
func racingBackends(t *testing.T) map[string]func(*config.Config) {
	backends := map[string]func(*config.Config){"SQLite": func(*config.Config) {}}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends["PostgreSQL"] = usePostgres(t, dsn)
	}
	return backends
}

// newPostgresApp is an app on PostgreSQL, for a test that stops a write halfway
func newPostgresApp(t *testing.T, options ...func(*config.Config)) *app {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	return newAppWithAdmin(t, append([]func(*config.Config){usePostgres(t, dsn)}, options...)...)
}

// midway marks the context of the write a test stops halfway
type midway struct{}

// stopMidway is a query hook. After the first query of the marked write that matches, it runs the
// other writes, each in its own goroutine, while the marked write's transaction is still open. It
// gives them a second to finish. One that waits for the workspace the marked write holds won't,
// and goes on once the marked write commits.
type stopMidway struct {
	matches func(query string) bool
	others  []func()
	fired   atomic.Bool
	done    sync.WaitGroup
}

func (s *stopMidway) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }

func (s *stopMidway) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if ctx.Value(midway{}) == nil || !s.matches(event.Query) || !s.fired.CompareAndSwap(false, true) {
		return
	}
	finished := make(chan struct{})
	var mine sync.WaitGroup
	for _, other := range s.others {
		mine.Add(1)
		s.done.Add(1)
		go func() {
			defer s.done.Done()
			defer mine.Done()
			other()
		}()
	}
	go func() { mine.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
	}
}

// hookQueries adds the hook to the app's database
func (a *app) hookQueries(t *testing.T, hook bun.QueryHook) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) { db.AddQueryHook(hook) }))
}

func (a *app) activeMonitors(t *testing.T) int {
	t.Helper()
	active := 0
	for _, m := range a.limitedMonitors(t) {
		if m.Active {
			active++
		}
	}
	return active
}

// AC5: resumes at once, by PATCH and by PUT, in a workspace one below its limit end with one of
// them resumed
func TestLimits_Monitors_ResumesAtOnceBelowTheLimitResumeOne(t *testing.T) {
	for name, backend := range racingBackends(t) {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, backend, withMonitorLimit(3))
			a.createHTTPMonitor(t, "One", "")
			a.createHTTPMonitor(t, "Two", "")
			const racers = 6
			paused := make([]string, racers)
			for i := range paused {
				paused[i] = decode[limitedMonitor](t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("Paused", ""), "active", false), a.asAdmin), http.StatusCreated).ID
			}

			codes := make([]int, racers)
			var wg sync.WaitGroup
			for i, id := range paused {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if i%2 == 0 {
						codes[i] = a.request(t, http.MethodPatch, monitorsPath+"/"+id, map[string]any{"active": true}, a.asAdmin).Code
					} else {
						codes[i] = a.request(t, http.MethodPut, monitorsPath+"/"+id, httpMonitorBody("Resumed", ""), a.asAdmin).Code
					}
				}()
			}
			wg.Wait()

			resumed := 0
			for _, code := range codes {
				if code == http.StatusOK {
					resumed++
				}
			}
			assert.Equal(t, 1, resumed, "%v", codes)
			assert.Equal(t, 3, a.activeMonitors(t))
		})
	}
}

// AC5: an edit of an active monitor that keeps it active, by PUT or PATCH, reads it after it holds
// the workspace. Read before, a pause of the monitor and a resume of another can both land between
// the read and the write, and the edit then makes the monitor active again past the limit.
func TestLimits_Monitors_AnEditThatKeepsAMonitorActiveCantPassTheLimit(t *testing.T) {
	active, name := true, "Edited again"
	for method, edit := range map[string]func(ctx context.Context, monitors monitor.Service, workspaceID workspace.ID, id string) error{
		http.MethodPut: func(ctx context.Context, monitors monitor.Service, workspaceID workspace.ID, id string) error {
			_, err := monitors.UpdateFull(ctx, workspaceID, id, &monitor.CreateUpdateDto{
				Type: "http", Name: name, Interval: 60, Timeout: 16, RetryInterval: 60, Active: true,
				Config: httpMonitorBody("Edited", "")["config"].(string),
			})
			return err
		},
		http.MethodPatch: func(ctx context.Context, monitors monitor.Service, workspaceID workspace.ID, id string) error {
			_, err := monitors.UpdatePartial(ctx, workspaceID, id, &monitor.PartialUpdateDto{Name: &name, Active: &active}, false)
			return err
		},
	} {
		t.Run(method, func(t *testing.T) {
			a := newPostgresApp(t, withMonitorLimit(1))
			edited := a.createHTTPMonitor(t, "Edited", "")
			other := decode[limitedMonitor](t, a.request(t, http.MethodPost, monitorsPath, with(httpMonitorBody("Other", ""), "active", false), a.asAdmin), http.StatusCreated).ID
			workspaceID := a.firstWorkspace(t)

			var pauseCode, resumeCode int
			hook := &stopMidway{
				// The edit's read of the monitor
				matches: func(query string) bool {
					return strings.HasPrefix(query, "SELECT") && strings.Contains(query, "m.id = '"+edited+"'")
				},
				others: []func(){
					func() {
						pauseCode = a.request(t, http.MethodPatch, monitorsPath+"/"+edited, map[string]any{"active": false}, a.asAdmin).Code
						resumeCode = a.request(t, http.MethodPatch, monitorsPath+"/"+other, map[string]any{"active": true}, a.asAdmin).Code
					},
				},
			}
			a.hookQueries(t, hook)

			ctx := context.WithValue(context.Background(), midway{}, true)
			require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
				return edit(ctx, monitors, workspaceID, edited)
			}))
			hook.done.Wait()

			require.True(t, hook.fired.Load(), "the edit read the monitor")
			assert.Equal(t, http.StatusOK, pauseCode, "pausing is always allowed")
			assert.LessOrEqual(t, a.activeMonitors(t), 1, "resume: %d", resumeCode)
		})
	}
}

// AC4: probes assigned to every monitor at once, to a monitor one below its probe limit, end with
// the monitor at its limit
func TestLimits_Probes_AssignsAtOnceEndAtTheLimit(t *testing.T) {
	for name, backend := range racingBackends(t) {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, backend)
			const racers = 6
			probes := make([]string, racers)
			for i := range probes {
				probes[i] = a.probeThatRuns(t, "Probe", "http").ID
			}
			site := a.createHTTPMonitor(t, "Site", "")
			a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{ProbesPerMonitor: limitOf(2)})

			var wg sync.WaitGroup
			for _, probe := range probes {
				wg.Add(1)
				go func() {
					defer wg.Done()
					a.assignAllMonitors(t, probe)
				}()
			}
			wg.Wait()

			assert.Len(t, a.monitorProbes(t, site), 2)
		})
	}
}

// AC4: a probe assigned to every monitor counts a monitor's probes after it holds the workspace, and
// adds the probe before another assignment or an update of the monitor's probes counts them
func TestLimits_Probes_AnAssignmentCantPassTheLimitBesideAnotherWrite(t *testing.T) {
	for name, other := range map[string]func(t *testing.T, a *app, site, probe string){
		"another assignment": func(t *testing.T, a *app, _ string, probe string) {
			require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
				_, err := monitors.AssignProbeToAllAnyWorkspace(context.Background(), probe, nil, nil)
				return err
			}))
		},
		"an update of the monitor's probes": func(t *testing.T, a *app, site string, probe string) {
			a.request(t, http.MethodPatch, monitorsPath+"/"+site, map[string]any{"probe_ids": []string{"local", probe}}, a.asAdmin)
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := newPostgresApp(t)
			frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
			paris := a.probeThatRuns(t, "Paris", "http")
			site := a.createHTTPMonitor(t, "Site", "")
			a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{ProbesPerMonitor: limitOf(2)})

			hook := &stopMidway{
				// The assignment's count of the monitors' probes
				matches: func(query string) bool { return strings.Contains(query, "AS probes") },
				others:  []func(){func() { other(t, a, site, paris.ID) }},
			}
			a.hookQueries(t, hook)

			ctx := context.WithValue(context.Background(), midway{}, true)
			require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
				_, err := monitors.AssignProbeToAllAnyWorkspace(ctx, frankfurt.ID, nil, nil)
				return err
			}))
			hook.done.Wait()

			require.True(t, hook.fired.Load(), "the assignment counted the probes")
			assert.Len(t, a.monitorProbes(t, site), 2)
		})
	}
}

// heldWorkspaces is a query hook that records, for each transaction of the marked write, the
// workspaces it holds (limits.LockWorkspace), in the order it holds them
type heldWorkspaces struct {
	mu       sync.Mutex
	attempts [][]string
}

// lockedWorkspace is the query of limits.LockWorkspace, with the workspace it holds
var lockedWorkspace = regexp.MustCompile(`^SELECT "id" FROM "workspaces" WHERE \(id = '([^']+)'\) FOR UPDATE$`)

func (h *heldWorkspaces) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (h *heldWorkspaces) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if ctx.Value(midway{}) == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if event.Query == "BEGIN" {
		h.attempts = append(h.attempts, []string{})
	} else if found := lockedWorkspace.FindStringSubmatch(event.Query); found != nil && len(h.attempts) > 0 {
		h.attempts[len(h.attempts)-1] = append(h.attempts[len(h.attempts)-1], found[1])
	}
}

// AC4: an assignment holds the workspaces with a probe limit in id order. A monitor created, once
// the assignment holds its workspaces, in a workspace that sorts below them isn't held out of
// order: the assignment is made again from the start, and gives that monitor the probe too, within
// its limit.
func TestLimits_Probes_AnAssignmentHoldsItsWorkspacesInOrderWhenAMonitorIsCreatedBelow(t *testing.T) {
	a := newPostgresApp(t)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	first, second := a.firstWorkspace(t), a.createWorkspace(t, "Second")
	a.addMember(t, second, a.userID(t, adminEmail), workspace.RoleOwner)
	lower, higher := min(first, second), max(first, second)
	for _, id := range []workspace.ID{lower, higher} {
		a.setOwnLimits(t, id, &limits.Limits{ProbesPerMonitor: limitOf(2)})
	}
	existing := decode[created](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Existing", ""), a.asAdmin, inWorkspace(higher)), http.StatusCreated).ID

	held := &heldWorkspaces{}
	a.hookQueries(t, held)
	var createdBelow *httpRecorder
	hook := &stopMidway{
		// The assignment holds the workspace of the one monitor there is
		matches: func(query string) bool { return lockedWorkspace.MatchString(query) },
		others: []func(){func() {
			createdBelow = a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Created below", ""), a.asAdmin, inWorkspace(lower))
		}},
	}
	a.hookQueries(t, hook)

	ctx := context.WithValue(context.Background(), midway{}, true)
	var added int
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		var err error
		added, err = monitors.AssignProbeToAllAnyWorkspace(ctx, frankfurt.ID, nil, nil)
		return err
	}))
	hook.done.Wait()

	require.True(t, hook.fired.Load(), "the assignment held a workspace")
	below := decode[created](t, createdBelow, http.StatusCreated).ID
	held.mu.Lock()
	attempts := held.attempts
	held.mu.Unlock()
	for i, workspaces := range attempts {
		assert.True(t, slices.IsSorted(workspaces), "attempt %d holds its workspaces in id order: %v", i+1, workspaces)
	}
	assert.Equal(t, [][]string{{string(higher)}, {string(lower), string(higher)}}, attempts, "the assignment is made again, holding both")
	assert.Equal(t, 2, added)
	probesIn := func(id workspace.ID, monitorID string) []string {
		return decode[monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath+"/"+monitorID, nil, a.asAdmin, inWorkspace(id)), http.StatusOK).ProbeIDs
	}
	assert.ElementsMatch(t, []string{"local", frankfurt.ID}, probesIn(higher, existing))
	assert.ElementsMatch(t, []string{"local", frankfurt.ID}, probesIn(lower, below))
}
