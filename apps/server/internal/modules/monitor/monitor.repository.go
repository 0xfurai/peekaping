package monitor

import (
	"context"

	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
)

// MonitorRepository stores monitors, which belong to a workspace (docs/adr/0005 section 3).
//
// A method that serves a workspace's request takes the workspace right after the context. It reads
// the workspace's monitors only, and a write to a monitor of another workspace matches no row and
// is reported as ErrMonitorNotFound.
//
// A method whose name ends in AnyWorkspace reads or writes across workspaces, for the paths that
// run for the whole install: the scheduler, the push endpoint, the probe protocol, notifications,
// public status pages and badges, /metrics, the usage ping and the jobs at startup. Each one sits
// next to its scoped twin, and a new caller needs a reason in review.
type MonitorRepository interface {
	Create(ctx context.Context, workspaceID workspace.ID, monitor *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	// FindWorkspaceIDAnyWorkspace returns the workspace the monitor is in, and false when there is
	// no such monitor. It reads that one column, for the paths that only route by workspace, such
	// as the websocket. A monitor never changes workspace.
	FindWorkspaceIDAnyWorkspace(ctx context.Context, id string) (workspace.ID, bool, error)
	FindByIDs(ctx context.Context, workspaceID workspace.ID, ids []string) ([]*Model, error)
	FindByIDsAnyWorkspace(ctx context.Context, ids []string) ([]*Model, error)
	FindAll(
		ctx context.Context,
		workspaceID workspace.ID,
		page int,
		limit int,
		q string,
		active *bool,
		status *int,
		tagIds []string,
	) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every monitor of the install, paused ones included
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	FindActiveAnyWorkspace(ctx context.Context) ([]*Model, error)
	FindActivePaginatedAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	// FindActiveByProbeAnyWorkspace returns a page of the active monitors the probe runs. A probe
	// belongs to the install, so it runs the monitors of every workspace.
	FindActiveByProbeAnyWorkspace(ctx context.Context, probeID string, page int, limit int) ([]*Model, error)
	// AddProbeToAllAnyWorkspace adds the probe to every monitor of the install that isn't a push
	// monitor, of one of the types (every type when there are none), and doesn't run on it yet,
	// and returns the ids of the monitors it changed. A changed monitor has a new updated_at, and
	// one that had no probe rows, and so ran on the local probe, gets that row as well. Call it in
	// a transaction: the rows are many.
	AddProbeToAllAnyWorkspace(ctx context.Context, probeID string, supportedTypes []string) ([]string, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, monitor *Model) error
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, monitor *UpdateModel) error
	// UpdatePartialAnyWorkspace is UpdatePartial for background work, which knows the monitor by its
	// id alone. A monitor that is gone is not an error.
	UpdatePartialAnyWorkspace(ctx context.Context, id string, monitor *UpdateModel) error
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
	// RemoveProxyReference clears the proxy from the workspace's monitors that use it, which get a
	// new updated_at
	RemoveProxyReference(ctx context.Context, workspaceID workspace.ID, proxyId string) error
	// ProxyInWorkspace is whether the proxy exists and belongs to the workspace
	ProxyInWorkspace(ctx context.Context, workspaceID workspace.ID, proxyID string) (bool, error)
	FindByProxyId(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error)
	FindOneByPushTokenAnyWorkspace(ctx context.Context, pushToken string) (*Model, error)
	// RecordLocationAnyWorkspace stores what a result left on the monitor's row for the probe that
	// ran it, then calls judge with the monitor's last recorded status and its rows from just
	// before and just after, and records the status judge returns as the monitor's (nil clears
	// it). All of it happens with the monitor held, so two results of one monitor see one another
	// in turn: each one's recorded status is what the other's judge returned. A monitor without a
	// row for the probe has nothing stored on the row, and one without rows is judged on none. A monitor
	// of a suspended workspace has nothing stored and nothing judged: shared.ErrWorkspaceSuspended. So does
	// a monitor of a disabled type: shared.ErrMonitorTypeDisabled.
	RecordLocationAnyWorkspace(ctx context.Context, monitorID string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error
	// ResetLocations forgets where the monitor stood: its probes' rows lose their state and the
	// monitor loses its recorded status, so that its next result is its first. It is
	// ErrMonitorNotFound for a monitor of another workspace.
	ResetLocations(ctx context.Context, workspaceID workspace.ID, monitorID string) error
}
