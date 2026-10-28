package monitor_maintenance

import (
	"context"

	"peekaping/internal/modules/workspace"
)

type Repository interface {
	Create(ctx context.Context, model *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	FindByMaintenanceID(ctx context.Context, maintenanceID string) ([]*Model, error)
	Delete(ctx context.Context, id string) error
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByMaintenanceID(ctx context.Context, maintenanceID string) error
	// FindMonitorIDsInWorkspace returns which of the monitors are in the workspace. The ID of a
	// monitor of another workspace, or of none, is left out.
	FindMonitorIDsInWorkspace(ctx context.Context, workspaceID workspace.ID, ids []string) ([]string, error)
}
