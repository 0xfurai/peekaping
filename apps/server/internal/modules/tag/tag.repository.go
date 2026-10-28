package tag

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// Repository stores tags, which belong to a workspace (docs/adr/0005 section 3). Every method
// takes the workspace right after the context, filters on it, and treats another workspace's tag as
// one that doesn't exist: a find returns nil, and a write that changes no row is ErrNotFound.
type Repository interface {
	// Create stores the tag in the workspace, whatever workspace the tag says
	Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) error
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
	// FindByName finds the workspace's tag with the name
	FindByName(ctx context.Context, workspaceID workspace.ID, name string) (*Model, error)
}
