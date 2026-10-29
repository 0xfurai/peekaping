package monitor_status_page

import (
	"context"

	"peekaping/internal/modules/workspace"
)

type Repository interface {
	Create(ctx context.Context, entity *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error

	// Additional methods for managing relationships
	AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*Model, error)
	RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error
	GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error)
	GetStatusPagesForMonitor(ctx context.Context, monitorID string) ([]*Model, error)
	FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*Model, error)
	UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*Model, error)
	UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*Model, error)
	DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error
	// FindMonitorIDsInWorkspace is the ids among ids that name a monitor of the workspace
	FindMonitorIDsInWorkspace(ctx context.Context, workspaceID workspace.ID, ids []string) ([]string, error)
}
