package maintenance

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// Repository stores maintenance windows, which belong to a workspace (docs/adr/0005 section 3). A
// method that serves a workspace's request takes the workspace right after the context, filters on
// it, and treats another workspace's window as one that doesn't exist: a find returns nil, and a
// write that changes no row is ErrNotFound. A method whose name ends in AnyWorkspace works across
// workspaces.
type Repository interface {
	// Create stores the window in the workspace, whatever workspace the window says
	Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, strategy string) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every window of the install, for the usage ping
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error

	SetActive(ctx context.Context, workspaceID workspace.ID, id string, active bool) (*Model, error)
	// GetMaintenancesByMonitorIDAnyWorkspace returns the active windows of a monitor, for the
	// checks that decide whether the monitor is under maintenance. The monitor is the key, and its
	// windows are in its workspace by section 2.
	GetMaintenancesByMonitorIDAnyWorkspace(ctx context.Context, monitorID string) ([]*Model, error)
}
