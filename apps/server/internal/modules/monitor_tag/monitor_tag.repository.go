package monitor_tag

import (
	"context"

	"peekaping/internal/modules/workspace"
)

type Repository interface {
	Create(ctx context.Context, model *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	// FindByMonitorIDs returns the links of all the monitors by monitor ID, each list ordered as
	// FindByMonitorID orders it. A monitor with no links has no entry.
	FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error)
	FindByTagID(ctx context.Context, tagID string) ([]*Model, error)
	Delete(ctx context.Context, id string) error
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByTagID(ctx context.Context, tagID string) error
	DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error
	// FindTagIDsInWorkspace returns which of the tags are in the workspace. The ID of a tag of
	// another workspace, or of none, is left out.
	FindTagIDsInWorkspace(ctx context.Context, workspaceID workspace.ID, ids []string) ([]string, error)
}
