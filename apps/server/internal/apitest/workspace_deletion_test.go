package apitest

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-170: an owner deletes their workspace by typing its name (docs/adr/0005 section 2). The
// workspace is held for 7 days, as a suspended one is, and then it and everything in it is deleted.

const deletionPath = "/api/v1/workspace/deletion"

type scheduledDeletion struct {
	DeletionScheduledAt time.Time `json:"deletionScheduledAt"`
}

func (a *app) scheduleDeletion(t *testing.T, name string, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, deletionPath, map[string]any{"name": name}, creds...)
}

func (a *app) cancelDeletion(t *testing.T, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodDelete, deletionPath, nil, creds...)
}

// purge runs the cleanup job's deletion of the workspaces whose 7 days are over, as the hourly run
// does at now
func (a *app) purge(t *testing.T, now time.Time) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository, service workspace.Service) {
		cleanup.PurgeScheduledWorkspaces(context.Background(), workspaces, service, now, zap.NewNop().Sugar())
	}))
}

// MARK: Scheduling

func TestWorkspaceDeletion_AnOwnerSchedulesItByTypingTheNameAndTheDateIs7DaysOut(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedWorkspace(t, "Doomed", "doomed")
		before := time.Now()

		got := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Doomed", w.owner), http.StatusOK)

		assert.WithinDuration(t, before.Add(7*24*time.Hour), got.DeletionScheduledAt, time.Minute)
		for table, n := range a.footprint(t, w) {
			assert.Positive(t, n, "scheduling deletes nothing yet: "+table)
		}
	})
}

func TestWorkspaceDeletion_ANameThatDoesNotMatchSchedulesNothing(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Doomed", "doomed")

	for _, name := range []string{"doomed", "Doomed ", "Doom", "", "Another"} {
		res := a.scheduleDeletion(t, name, w.owner)
		assert.Equal(t, http.StatusBadRequest, res.Code, name)
		assert.Contains(t, res.Body.String(), "WORKSPACE_NAME_MISMATCH", name)
	}

	assert.Nil(t, a.meOf(t, w.owner).Workspaces[0].DeletionScheduledAt)
}

func TestWorkspaceDeletion_OnlyAnOwnerSchedulesIt(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Doomed", "doomed")
	admin := a.userIn(t, "doomed-admin@example.com", w.id, workspace.RoleAdmin)
	key := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "ci"}, w.owner), http.StatusCreated).Token

	assert.Equal(t, http.StatusForbidden, a.scheduleDeletion(t, "Doomed", admin).Code, "an admin")
	assert.Equal(t, http.StatusForbidden, a.scheduleDeletion(t, "Doomed", w.member).Code, "a member")
	assert.Equal(t, http.StatusUnauthorized, a.scheduleDeletion(t, "Doomed", withHeader("X-API-Key", key)).Code, "an API key, as for leaving")
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, deletionPath, map[string]any{"name": "Doomed"}).Code, "no sign-in")

	assert.Nil(t, a.meOf(t, w.owner).Workspaces[0].DeletionScheduledAt)
	assert.Equal(t, http.StatusForbidden, a.cancelDeletion(t, admin).Code, "an admin can't cancel")
}

func TestWorkspaceDeletion_SchedulingAgainKeepsTheFirstDate(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Doomed", "doomed")

	first := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Doomed", w.owner), http.StatusOK)
	time.Sleep(5 * time.Millisecond)
	second := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Doomed", w.owner), http.StatusOK)

	assert.True(t, first.DeletionScheduledAt.Equal(second.DeletionScheduledAt), "%s vs %s", first.DeletionScheduledAt, second.DeletionScheduledAt)
}

func TestWorkspaceDeletion_SchedulingAndCancellingAreLoggedWithWhoDidWhatToWhich(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	w := a.seedWorkspace(t, "Doomed", "doomed")
	ownerID := a.userID(t, w.ownerEmail)

	require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Doomed", w.owner).Code)
	require.Equal(t, http.StatusNoContent, a.cancelDeletion(t, w.owner).Code)

	entries := logs.FilterMessage("Workspace action").All()
	require.Len(t, entries, 2)
	for i, action := range []string{"schedule_deletion", "cancel_deletion"} {
		fields := entries[i].ContextMap()
		assert.Len(t, fields, 3, "who, what and to which workspace, and nothing else")
		assert.Equal(t, ownerID, fields["actorUserId"])
		assert.Equal(t, action, fields["action"])
		assert.Equal(t, string(w.id), fields["targetWorkspaceId"])
	}
}

// MARK: Purge

func TestWorkspaceDeletion_AfterThe7DaysNoRowOfTheWorkspaceIsLeftAndNoneOfAnothers(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		doomed := a.seedWorkspace(t, "Doomed", "doomed")
		kept := a.seedWorkspace(t, "Kept", "kept")
		keptBefore := a.footprint(t, kept)
		due := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Doomed", doomed.owner), http.StatusOK).DeletionScheduledAt

		a.purge(t, due.Add(-24*time.Hour))
		for table, n := range a.footprint(t, doomed) {
			assert.Positive(t, n, "6 days in, it is still there: "+table)
		}

		a.purge(t, due.Add(time.Minute))
		for table, n := range a.footprint(t, doomed) {
			assert.Zero(t, n, "7 days on, nothing is left in "+table)
		}
		assert.Equal(t, keptBefore, a.footprint(t, kept), "another workspace keeps every row")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, monitorsPath+"/"+kept.monitors[0], nil, kept.owner).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, monitorsPath, nil, doomed.owner).Code, "the owner is in no workspace now")
		assert.Equal(t, http.StatusOK, a.login(t, userLogin(doomed.ownerEmail)).Code, "and keeps the account")
	})
}

func TestWorkspaceDeletion_AWorkspaceThatIsNotScheduledIsNeverPurged(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedWorkspace(t, "Kept", "kept")
	before := a.footprint(t, w)

	a.purge(t, time.Now().Add(365*24*time.Hour))

	assert.Equal(t, before, a.footprint(t, w))
}

func TestWorkspaceDeletion_OneWorkspaceThatCantBeDeletedDoesNotStopTheOthers(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.seedWorkspace(t, "First", "first")
	second := a.seedWorkspace(t, "Second", "second")
	var due time.Time
	for name, w := range map[string]seededWorkspace{"First": first, "Second": second} {
		due = decode[scheduledDeletion](t, a.scheduleDeletion(t, name, w.owner), http.StatusOK).DeletionScheduledAt
	}
	core, logs := observer.New(zap.InfoLevel)

	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository, service workspace.Service) {
		cleanup.PurgeScheduledWorkspaces(context.Background(), workspaces, failingFor{Service: service, fail: first.id}, due.Add(time.Minute), zap.New(core).Sugar())
	}))

	for table, n := range a.footprint(t, second) {
		assert.Zero(t, n, "the second is deleted: "+table)
	}
	assert.Positive(t, a.footprint(t, first)["workspaces"], "the first is left for the next hour")
	assert.Len(t, logs.FilterMessage("Failed to delete a workspace scheduled for deletion").All(), 1)
	assert.Len(t, logs.FilterMessage("Deleted a workspace scheduled for deletion").All(), 1)
}

// changedAfterListing runs a change once the purge has listed the workspaces that are due, and
// before it deletes the first of them: the gap in which an owner can cancel
type changedAfterListing struct {
	workspace.Repository
	change func()
	listed *[]workspace.ID
}

func (c changedAfterListing) ListDeletionDueAnyWorkspace(ctx context.Context, now time.Time) ([]workspace.ID, error) {
	due, err := c.Repository.ListDeletionDueAnyWorkspace(ctx, now)
	*c.listed = due
	c.change()
	return due, err
}

func TestWorkspaceDeletion_AWorkspaceCancelledAfterTheListingIsNotDeleted(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		cases := map[string]bool{"cancelled": false, "cancelled and scheduled again": true}
		for name, again := range cases {
			t.Run(name, func(t *testing.T) {
				a := newAppWithAdmin(t, backend)
				saved := a.seedWorkspace(t, "Saved", "saved")
				due := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Saved", saved.owner), http.StatusOK).DeletionScheduledAt
				before := a.footprint(t, saved)
				core, logs := observer.New(zap.InfoLevel)
				var listed []workspace.ID

				require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository, service workspace.Service) {
					stale := changedAfterListing{Repository: workspaces, listed: &listed, change: func() {
						require.Equal(t, http.StatusNoContent, a.cancelDeletion(t, saved.owner).Code)
						if again {
							require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Saved", saved.owner).Code)
						}
					}}
					// At the date itself: the new schedule of the second case is later, so it is not due
					cleanup.PurgeScheduledWorkspaces(context.Background(), stale, service, due, zap.New(core).Sugar())
				}))

				assert.Equal(t, []workspace.ID{saved.id}, listed, "it was due when the purge listed it")
				assert.Equal(t, before, a.footprint(t, saved), "every row is kept")
				assert.Empty(t, logs.FilterMessage("Deleted a workspace scheduled for deletion").All())
				assert.Empty(t, logs.FilterMessage("Failed to delete a workspace scheduled for deletion").All(), "a workspace that is no longer due is no error")
			})
		}
	})
}

type failingFor struct {
	workspace.Service
	fail workspace.ID
}

func (f failingFor) DeleteDueAnyWorkspace(ctx context.Context, id workspace.ID, now time.Time) (bool, error) {
	if id == f.fail {
		return false, assert.AnError
	}
	return f.Service.DeleteDueAnyWorkspace(ctx, id, now)
}

// MARK: The hold

// assertHeld checks everything that a hold keeps from running in the Acme workspace: the scheduler,
// the loader, the queries that pick monitors, the push, the status pages and the badges
func assertHeld(t *testing.T, a *app, w acme, source *scheduler.DatabaseSource) {
	t.Helper()
	assert.Empty(t, intersect(scheduled(t, source), w.monitorsOfWorkspace), "the scheduler's list")
	found, err := source.Find(context.Background(), w.activeA)
	require.NoError(t, err)
	assert.Nil(t, found)
	_, err = source.Load(context.Background(), w.activeA)
	assert.ErrorIs(t, err, check.ErrWorkspaceSuspended, "the loader")
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		active, err := monitors.FindActiveAnyWorkspace(context.Background())
		require.NoError(t, err)
		assert.Empty(t, intersect(idsOf(active), w.monitorsOfWorkspace))
		paged, err := monitors.FindActivePaginatedAnyWorkspace(context.Background(), 0, 1000)
		require.NoError(t, err)
		assert.Empty(t, intersect(idsOf(paged), w.monitorsOfWorkspace))
		byProbe, err := monitors.FindActiveByProbeAnyWorkspace(context.Background(), "local", 0, 1000)
		require.NoError(t, err)
		assert.Empty(t, intersect(idsOf(byProbe), w.monitorsOfWorkspace))
		return nil
	}))
	push := a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil)
	assert.Equal(t, http.StatusForbidden, push.Code, push.Body.String())
	assert.Empty(t, a.storedHeartbeats(t, w.activeA))
	for _, path := range publicStatusPageRoutes(w.slugWithDomain, w.domain) {
		assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
	}
	for path := range badgePaths(w.activeA) {
		assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
	}
}

// assertRunning checks that the Acme workspace runs: the opposite of assertHeld
func assertRunning(t *testing.T, a *app, w acme, source *scheduler.DatabaseSource) {
	t.Helper()
	assert.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(scheduled(t, source), w.monitorsOfWorkspace), "the paused one stays out")
	loaded, err := source.Load(context.Background(), w.activeA)
	require.NoError(t, err)
	assert.Equal(t, w.activeA, loaded.Monitor.ID)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil).Code)
	for _, path := range publicStatusPageRoutes(w.slugWithDomain, w.domain) {
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, path)
	}
	for path := range badgePaths(w.activeA) {
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, path)
	}
}

func TestWorkspaceDeletion_TheWorkspaceIsHeldDuringTheGracePeriodAndEveryMemberSeesTheDate(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		member := a.loginAs(t, w.memberEmail)
		source := a.schedulerSource(t)
		require.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(scheduled(t, source), w.monitorsOfWorkspace), "they run before")

		due := decode[scheduledDeletion](t, a.scheduleDeletion(t, "Acme Corp", w.alice), http.StatusOK).DeletionScheduledAt

		assertHeld(t, a, w, source)
		push := a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil)
		assert.Contains(t, push.Body.String(), "scheduled for deletion")
		assert.NotContains(t, push.Body.String(), "verifies")
		for _, creds := range []credentials{member, w.alice, w.bob} {
			me := a.meOf(t, creds)
			require.Len(t, me.Workspaces, 1)
			assert.True(t, me.Workspaces[0].Suspended)
			require.NotNil(t, me.Workspaces[0].SuspendedReason)
			assert.Equal(t, "deleting", *me.Workspaces[0].SuspendedReason)
			require.NotNil(t, me.Workspaces[0].DeletionScheduledAt)
			assert.True(t, due.Equal(mustParse(t, *me.Workspaces[0].DeletionScheduledAt)), "the date is the one scheduling answered")
		}
		listed := decode[[]monitorViewWithFlag](t, a.request(t, http.MethodGet, monitorsPath, nil, w.alice), http.StatusOK)
		assert.Len(t, listed, 3, "the monitors are kept and listed")
		assert.Equal(t, "deleting", derefOr(a.adminWorkspaceView(t, w.id).SuspendedReason, ""), "the admin list says so too")
	})
}

func derefOr(s *string, or string) string {
	if s == nil {
		return or
	}
	return *s
}

func TestWorkspaceDeletion_CancellingBringsEverythingBack(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		source := a.schedulerSource(t)
		var heard updates
		source.Watch(func(string) {}, heard.add, func(string) {})
		w := a.createAcme(t)
		// pausing a monitor is an update too, and it can arrive late: wait for it
		require.Eventually(t, func() bool { return slices.Contains(heard.seen(), w.paused) }, 5*time.Second, 10*time.Millisecond)
		heard.reset()
		updatedBefore := a.monitorUpdatedAt(t, w.activeA)
		require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Acme Corp", w.alice).Code)
		time.Sleep(5 * time.Millisecond)

		require.Equal(t, http.StatusNoContent, a.cancelDeletion(t, w.bob).Code, "another owner may cancel")

		assertRunning(t, a, w, source)
		me := a.meOf(t, w.alice)
		assert.False(t, me.Workspaces[0].Suspended)
		assert.Nil(t, me.Workspaces[0].SuspendedReason)
		assert.Nil(t, me.Workspaces[0].DeletionScheduledAt)
		assert.True(t, a.monitorUpdatedAt(t, w.activeA).After(updatedBefore), "updated_at moved, so the watchdog does not count the days held back")
		require.Eventually(t, func() bool { return len(intersect(heard.seen(), w.monitorsOfWorkspace)) >= 2 }, 5*time.Second, 10*time.Millisecond, "the scheduler is told to pick the monitors up")
		assert.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(heard.seen(), w.monitorsOfWorkspace))

		// and it can be scheduled again, for a new date
		assert.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Acme Corp", w.alice).Code)
	})
}

func TestWorkspaceDeletion_CancellingWhenNothingIsScheduledIs404(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.createAcme(t)

	res := a.cancelDeletion(t, w.alice)

	assert.Equal(t, http.StatusNotFound, res.Code, res.Body.String())
}

func TestWorkspaceDeletion_CancellingLiftsOnlyItsOwnHold(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		source := a.schedulerSource(t)
		reason := func(creds credentials) string {
			me := a.meOf(t, creds)
			return derefOr(me.Workspaces[0].SuspendedReason, "")
		}
		a.setSuspended(t, w.id, true)
		require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Acme Corp", w.alice).Code)
		assert.Equal(t, "deleting", reason(w.alice), "the deletion is told before the admin's hold")

		require.Equal(t, http.StatusNoContent, a.cancelDeletion(t, w.alice).Code)

		assert.Equal(t, "admin", reason(w.alice), "the admin's hold stays")
		assert.Nil(t, a.meOf(t, w.alice).Workspaces[0].DeletionScheduledAt)
		assertHeld(t, a, w, source)
		push := a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil)
		assert.Contains(t, push.Body.String(), "suspended by the install's administrator")

		// and resuming while a deletion is scheduled leaves the workspace held
		require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Acme Corp", w.alice).Code)
		resumed := a.setSuspended(t, w.id, false)
		assert.True(t, resumed.Suspended)
		assert.Equal(t, "deleting", derefOr(resumed.SuspendedReason, ""))
		assertHeld(t, a, w, source)
	})
}

func TestWorkspaceDeletion_CancellingLeavesAWorkspaceThatWaitsForVerificationWaiting(t *testing.T) {
	a := newAppWithAdmin(t)
	id, owner := a.createUnverifiedWorkspace(t, "Waiting", "wait@example.com")
	monitorID := a.createMonitorAs(t, owner, "Site", "waiting-push")
	source := a.schedulerSource(t)
	require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Waiting", owner).Code)
	_ = id

	require.Equal(t, http.StatusNoContent, a.cancelDeletion(t, owner).Code)

	me := a.meOf(t, owner)
	assert.Equal(t, "unverified", derefOr(me.Workspaces[0].SuspendedReason, ""))
	assert.NotContains(t, scheduled(t, source), monitorID)
}
