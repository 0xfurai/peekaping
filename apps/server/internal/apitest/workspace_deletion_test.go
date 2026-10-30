package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/cleanup"
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

type failingFor struct {
	workspace.Service
	fail workspace.ID
}

func (f failingFor) DeleteAnyWorkspace(ctx context.Context, id workspace.ID) error {
	if id == f.fail {
		return assert.AnError
	}
	return f.Service.DeleteAnyWorkspace(ctx, id)
}
