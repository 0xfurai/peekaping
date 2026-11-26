package notification_channel

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// Repository stores notification channels, which belong to a workspace (docs/adr/0005 section 3).
// A method that serves a workspace's request takes the workspace right after the context, filters
// on it, and treats another workspace's channel as one that doesn't exist: a find returns nil, and
// a write that changes no row is ErrNotFound. A method whose name ends in AnyWorkspace works
// across workspaces.
type Repository interface {
	// Create stores the channel in the workspace, whatever workspace the channel says
	Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	// FindByIDAnyWorkspace is for the paths that send a notification to the channels linked to a
	// monitor or a probe
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every channel of the install, for the usage ping
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) error
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
}
