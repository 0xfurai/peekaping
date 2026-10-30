package monitor

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeLimitsSource gives every workspace the same limits, or fails, and keeps the workspaces it was
// asked for.
type fakeLimitsSource struct {
	limits limits.Limits
	err    error
	asked  *[]workspace.ID
}

func (f fakeLimitsSource) For(_ context.Context, workspaceID workspace.ID) (limits.Limits, limits.Origin, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, workspaceID)
	}
	return f.limits, limits.OriginWorkspace, f.err
}

func (f fakeLimitsSource) Snapshot(context.Context) (limits.Snapshot, error) {
	return limits.Snapshot{}, f.err
}

// tracingRepository notes the order of the calls that read the monitor, hold the workspace and
// write, and answers the lock and the count of active monitors as the test says. The rest goes to
// the mock.
type tracingRepository struct {
	*MockMonitorRepository
	trace    []string
	lockErr  error
	countErr error
	active   int
}

func (r *tracingRepository) LockWorkspace(context.Context, workspace.ID) error {
	r.trace = append(r.trace, "lock")
	return r.lockErr
}

func (r *tracingRepository) CountActive(context.Context, workspace.ID) (int, error) {
	r.trace = append(r.trace, "count")
	return r.active, r.countErr
}

func (r *tracingRepository) Create(ctx context.Context, workspaceID workspace.ID, monitor *Model) (*Model, error) {
	r.trace = append(r.trace, "create")
	return r.MockMonitorRepository.Create(ctx, workspaceID, monitor)
}

func (r *tracingRepository) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	r.trace = append(r.trace, "find")
	return r.MockMonitorRepository.FindByID(ctx, workspaceID, id)
}

func (r *tracingRepository) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	r.trace = append(r.trace, "find any")
	return r.MockMonitorRepository.FindByIDAnyWorkspace(ctx, id)
}

func (r *tracingRepository) FindWorkspaceIDAnyWorkspace(ctx context.Context, id string) (workspace.ID, bool, error) {
	r.trace = append(r.trace, "owner")
	return r.MockMonitorRepository.FindWorkspaceIDAnyWorkspace(ctx, id)
}

func (r *tracingRepository) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, monitor *Model) error {
	r.trace = append(r.trace, "update full")
	return r.MockMonitorRepository.UpdateFull(ctx, workspaceID, id, monitor)
}

func (r *tracingRepository) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, monitor *UpdateModel) error {
	r.trace = append(r.trace, "update partial")
	return r.MockMonitorRepository.UpdatePartial(ctx, workspaceID, id, monitor)
}

func (r *tracingRepository) UpdatePartialAnyWorkspace(ctx context.Context, id string, monitor *UpdateModel) error {
	r.trace = append(r.trace, "update partial any")
	return r.MockMonitorRepository.UpdatePartialAnyWorkspace(ctx, id, monitor)
}

// tracedService is a service whose workspaces all have the limits, over a repository that traces.
type tracedService struct {
	*MonitorServiceImpl
	repo  *tracingRepository
	mock  *MockMonitorRepository
	bus   *eventstest.Bus
	asked []workspace.ID
}

func newTracedService(l limits.Limits, limitsErr error) *tracedService {
	service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
	ts := &tracedService{MonitorServiceImpl: service, mock: mockRepo, bus: bus}
	ts.repo = &tracingRepository{MockMonitorRepository: mockRepo}
	service.monitorRepository = ts.repo
	service.limits = fakeLimitsSource{limits: l, err: limitsErr, asked: &ts.asked}
	return ts
}

func intPtr(n int) *int { return &n }

func (ts *tracedService) assertRolledBack(t *testing.T) {
	t.Helper()
	tx := transactionOf(ts.MonitorServiceImpl)
	assert.Equal(t, 0, tx.committed)
	assert.Equal(t, 1, tx.rolledBack)
	assert.Empty(t, ts.bus.Topics(), "nothing is published")
}

func TestMonitorService_CreateFailsBeforeItWrites(t *testing.T) {
	ctx := context.Background()
	dto := func() *CreateUpdateDto {
		return &CreateUpdateDto{Type: "http", Name: "Checkout", Interval: 60, RetryInterval: 60, Timeout: 16, Config: "{}", Active: true}
	}

	t.Run("the workspace's limits can't be read", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, errors.New("limits unavailable"))

		_, err := ts.Create(ctx, testWorkspace, dto())

		require.EqualError(t, err, "read the workspace's limits: limits unavailable")
		assert.Empty(t, ts.repo.trace)
		tx := transactionOf(ts.MonitorServiceImpl)
		assert.Equal(t, 0, tx.committed+tx.rolledBack, "no transaction was opened")
	})

	t.Run("the default probes can't be looked up", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.probes = fakeProbes{err: errors.New("probe lookup failed")}

		_, err := ts.Create(ctx, testWorkspace, dto())

		require.EqualError(t, err, "probe lookup failed")
		assert.Empty(t, ts.repo.trace)
		tx := transactionOf(ts.MonitorServiceImpl)
		assert.Equal(t, 0, tx.committed+tx.rolledBack, "no transaction was opened")
	})
}

func TestMonitorService_CreateHoldsTheWorkspaceBeforeItCounts(t *testing.T) {
	ctx := context.Background()
	dto := func(active bool) *CreateUpdateDto {
		return &CreateUpdateDto{Type: "http", Name: "Checkout", Interval: 60, RetryInterval: 60, Timeout: 16, Config: "{}", Active: active}
	}

	t.Run("an active monitor is counted, then written", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 4
		ts.mock.On("Create", inTx, testWorkspace, mock.Anything).Return(&Model{ID: "new"}, nil)

		_, err := ts.Create(ctx, testWorkspace, dto(true))

		require.NoError(t, err)
		assert.Equal(t, []string{"lock", "count", "create"}, ts.repo.trace)
		assert.Equal(t, 1, transactionOf(ts.MonitorServiceImpl).committed)
	})

	t.Run("a paused monitor is neither held nor counted", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 5
		ts.mock.On("Create", inTx, testWorkspace, mock.Anything).Return(&Model{ID: "new"}, nil)

		_, err := ts.Create(ctx, testWorkspace, dto(false))

		require.NoError(t, err)
		assert.Equal(t, []string{"create"}, ts.repo.trace)
	})

	t.Run("no limit on monitors holds nothing", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("Create", inTx, testWorkspace, mock.Anything).Return(&Model{ID: "new"}, nil)

		_, err := ts.Create(ctx, testWorkspace, dto(true))

		require.NoError(t, err)
		assert.Equal(t, []string{"create"}, ts.repo.trace)
	})

	t.Run("a workspace at its limit is refused", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 5

		_, err := ts.Create(ctx, testWorkspace, dto(true))

		var reached *limits.ReachedError
		require.ErrorAs(t, err, &reached)
		assert.Equal(t, 5, reached.Max)
		assert.Equal(t, []string{"lock", "count"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a lock that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.lockErr = errors.New("lock timeout")

		_, err := ts.Create(ctx, testWorkspace, dto(true))

		require.EqualError(t, err, "lock the workspace: lock timeout")
		assert.Equal(t, []string{"lock"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a count that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.countErr = errors.New("count failed")

		_, err := ts.Create(ctx, testWorkspace, dto(true))

		require.EqualError(t, err, "count the active monitors: count failed")
		assert.Equal(t, []string{"lock", "count"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})
}

func TestMonitorService_UpdateFullOrdersItsReadsAndLocks(t *testing.T) {
	ctx := context.Background()
	const id = "monitor123"
	dto := func(active bool) *CreateUpdateDto {
		return &CreateUpdateDto{Type: "http", Name: "Checkout", Interval: 60, RetryInterval: 60, Timeout: 16, Config: "{}", Active: active}
	}

	t.Run("the workspace's limits can't be read", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, errors.New("limits unavailable"))

		_, err := ts.UpdateFull(ctx, testWorkspace, id, dto(true))

		require.EqualError(t, err, "read the workspace's limits: limits unavailable")
		assert.Empty(t, ts.repo.trace)
		tx := transactionOf(ts.MonitorServiceImpl)
		assert.Equal(t, 0, tx.committed+tx.rolledBack, "no transaction was opened")
	})

	t.Run("an update that resumes the monitor holds the workspace before it reads the monitor", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 4
		stored := &Model{ID: id, Type: "http", Active: false}
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(stored, nil)
		ts.mock.On("UpdateFull", inTx, testWorkspace, id, mock.Anything).Return(nil)
		notifications, tags := ts.notificationsAndTags()
		notifications.On("DeleteByMonitorID", inTx, id).Return(nil)
		tags.On("DeleteByMonitorID", inTx, id).Return(nil)

		got, err := ts.UpdateFull(ctx, testWorkspace, id, dto(true))

		require.NoError(t, err)
		assert.Same(t, stored, got)
		assert.Equal(t, []string{"lock", "find", "lock", "count", "update full", "find"}, ts.repo.trace)
		assert.Equal(t, []workspace.ID{testWorkspace}, ts.asked)
	})

	t.Run("an update that stays paused holds nothing", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		stored := &Model{ID: id, Type: "http", Active: false}
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(stored, nil)
		ts.mock.On("UpdateFull", inTx, testWorkspace, id, mock.Anything).Return(nil)
		notifications, tags := ts.notificationsAndTags()
		notifications.On("DeleteByMonitorID", inTx, id).Return(nil)
		tags.On("DeleteByMonitorID", inTx, id).Return(nil)

		_, err := ts.UpdateFull(ctx, testWorkspace, id, dto(false))

		require.NoError(t, err)
		assert.Equal(t, []string{"find", "update full", "find"}, ts.repo.trace)
	})

	t.Run("a lock that fails stops before the monitor is read", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.lockErr = errors.New("lock timeout")

		_, err := ts.UpdateFull(ctx, testWorkspace, id, dto(true))

		require.EqualError(t, err, "lock the workspace: lock timeout")
		assert.Equal(t, []string{"lock"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a read of the monitor that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(nil, errors.New("database unavailable"))

		_, err := ts.UpdateFull(ctx, testWorkspace, id, dto(true))

		require.EqualError(t, err, "database unavailable")
		assert.Equal(t, []string{"find"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a read back that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(&Model{ID: id, Type: "http", Active: true}, nil).Once()
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(nil, errors.New("database unavailable")).Once()
		ts.mock.On("UpdateFull", inTx, testWorkspace, id, mock.Anything).Return(nil)
		notifications, tags := ts.notificationsAndTags()
		notifications.On("DeleteByMonitorID", inTx, id).Return(nil)
		tags.On("DeleteByMonitorID", inTx, id).Return(nil)

		_, err := ts.UpdateFull(ctx, testWorkspace, id, dto(true))

		require.EqualError(t, err, "database unavailable")
		assert.Equal(t, []string{"find", "update full", "find"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})
}

// notificationsAndTags are the mocks of the link services the traced service was built with.
func (ts *tracedService) notificationsAndTags() (*MockMonitorNotificationService, *MockMonitorTagService) {
	return ts.monitorNotificationService.(*MockMonitorNotificationService), ts.monitorTagService.(*MockMonitorTagService)
}

func TestMonitorService_UpdatePartialOrdersItsReadsAndLocks(t *testing.T) {
	ctx := context.Background()
	const id = "monitor123"
	other := workspace.ID("22222222-2222-2222-2222-222222222222")
	resume := func() *PartialUpdateDto { active := true; return &PartialUpdateDto{Active: &active} }
	rename := func() *PartialUpdateDto { name := "Renamed"; return &PartialUpdateDto{Name: &name} }

	t.Run("the workspace's limits can't be read", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, errors.New("limits unavailable"))

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, resume(), false)

		require.EqualError(t, err, "read the workspace's limits: limits unavailable")
		assert.Empty(t, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("an update that resumes the monitor holds the workspace before it reads the monitor", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 4
		stored := &Model{ID: id, WorkspaceID: testWorkspace, Type: "http", Active: false}
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(stored, nil)
		ts.mock.On("UpdatePartial", inTx, testWorkspace, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, resume(), false)

		require.NoError(t, err)
		assert.Equal(t, []string{"lock", "find", "lock", "count", "update partial", "find"}, ts.repo.trace)
		assert.Equal(t, []workspace.ID{testWorkspace}, ts.asked)
	})

	t.Run("a rename holds nothing", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(&Model{ID: id, WorkspaceID: testWorkspace, Type: "http"}, nil)
		ts.mock.On("UpdatePartial", inTx, testWorkspace, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, rename(), false)

		require.NoError(t, err)
		assert.Equal(t, []string{"find", "update partial", "find"}, ts.repo.trace)
	})

	t.Run("a lock that fails stops before the monitor is read", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.lockErr = errors.New("lock timeout")

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, resume(), false)

		require.EqualError(t, err, "lock the workspace: lock timeout")
		assert.Equal(t, []string{"lock"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a background update that resumes the monitor looks up its workspace, holds it, and reads the limits of that one", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.active = 4
		stored := &Model{ID: id, WorkspaceID: other, Type: "http", Active: false}
		ts.mock.On("FindWorkspaceIDAnyWorkspace", inTx, id).Return(other, true, nil)
		ts.mock.On("FindByIDAnyWorkspace", inTx, id).Return(stored, nil)
		ts.mock.On("UpdatePartialAnyWorkspace", inTx, id, mock.Anything).Return(nil)

		got, err := ts.UpdatePartialAnyWorkspace(ctx, id, resume(), true)

		require.NoError(t, err)
		assert.Same(t, stored, got)
		assert.Equal(t, []string{"owner", "lock", "find any", "lock", "count", "update partial any", "find any"}, ts.repo.trace)
		assert.Equal(t, []workspace.ID{other}, ts.asked, "the limits are read once, for the monitor's workspace")
	})

	t.Run("a background update that sets probes looks up its workspace too", func(t *testing.T) {
		ts := newTracedService(limits.Limits{ProbesPerMonitor: intPtr(2)}, nil)
		ts.mock.On("FindWorkspaceIDAnyWorkspace", inTx, id).Return(other, true, nil)
		ts.mock.On("FindByIDAnyWorkspace", inTx, id).Return(&Model{ID: id, WorkspaceID: other, Type: "http", ProbeIDs: []string{"local"}}, nil)
		ts.mock.On("UpdatePartialAnyWorkspace", inTx, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, &PartialUpdateDto{ProbeIDs: []string{"local"}}, true)

		require.NoError(t, err)
		assert.Equal(t, []string{"owner", "lock", "find any", "update partial any", "find any"}, ts.repo.trace)
	})

	t.Run("a background update that changes neither reads the limits after it has read the monitor", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.mock.On("FindByIDAnyWorkspace", inTx, id).Return(&Model{ID: id, WorkspaceID: other, Type: "http"}, nil)
		ts.mock.On("UpdatePartialAnyWorkspace", inTx, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, rename(), true)

		require.NoError(t, err)
		assert.Equal(t, []string{"find any", "update partial any", "find any"}, ts.repo.trace)
		assert.Equal(t, []workspace.ID{other}, ts.asked)
	})

	t.Run("a background update's workspace lookup that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindWorkspaceIDAnyWorkspace", inTx, id).Return(workspace.ID(""), false, errors.New("database unavailable"))

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, resume(), true)

		require.EqualError(t, err, "database unavailable")
		assert.Equal(t, []string{"owner"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a background update of a monitor that isn't there", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindWorkspaceIDAnyWorkspace", inTx, id).Return(workspace.ID(""), false, nil)

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, resume(), true)

		require.ErrorIs(t, err, ErrMonitorNotFound)
		assert.EqualError(t, err, "monitor not found: monitor monitor123")
		assert.Equal(t, []string{"owner"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a background update's lock that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{Monitors: intPtr(5)}, nil)
		ts.repo.lockErr = errors.New("lock timeout")
		ts.mock.On("FindWorkspaceIDAnyWorkspace", inTx, id).Return(other, true, nil)

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, resume(), true)

		require.EqualError(t, err, "lock the workspace: lock timeout")
		assert.Equal(t, []string{"owner", "lock"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a background update whose limits can't be read after the monitor is", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, errors.New("limits unavailable"))
		ts.mock.On("FindByIDAnyWorkspace", inTx, id).Return(&Model{ID: id, WorkspaceID: other, Type: "http"}, nil)

		_, err := ts.UpdatePartialAnyWorkspace(ctx, id, rename(), true)

		require.EqualError(t, err, "read the workspace's limits: limits unavailable")
		assert.Equal(t, []string{"find any"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a read back that fails", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(&Model{ID: id, WorkspaceID: testWorkspace, Type: "http"}, nil).Once()
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(nil, errors.New("database unavailable")).Once()
		ts.mock.On("UpdatePartial", inTx, testWorkspace, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, rename(), false)

		require.EqualError(t, err, "database unavailable")
		assert.Equal(t, []string{"find", "update partial", "find"}, ts.repo.trace)
		ts.assertRolledBack(t)
	})

	t.Run("a monitor gone when it is read back", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(&Model{ID: id, WorkspaceID: testWorkspace, Type: "http"}, nil).Once()
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(nil, nil).Once()
		ts.mock.On("UpdatePartial", inTx, testWorkspace, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, rename(), false)

		require.EqualError(t, err, "monitor not found: monitor monitor123")
		ts.assertRolledBack(t)
	})

	t.Run("publishes once after the commit", func(t *testing.T) {
		ts := newTracedService(limits.Limits{}, nil)
		stored := &Model{ID: id, WorkspaceID: testWorkspace, Type: "http"}
		ts.mock.On("FindByID", inTx, testWorkspace, id).Return(stored, nil)
		ts.mock.On("UpdatePartial", inTx, testWorkspace, id, mock.Anything).Return(nil)

		_, err := ts.UpdatePartial(ctx, testWorkspace, id, rename(), false)

		require.NoError(t, err)
		assert.Len(t, eventstest.Published(ts.bus, events.MonitorUpdated), 1)
	})
}
