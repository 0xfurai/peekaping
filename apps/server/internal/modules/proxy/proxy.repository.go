package proxy

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// Repository stores proxies, which belong to a workspace (docs/adr/0005 section 3). Every method
// takes the workspace right after the context, filters on it, and treats another workspace's proxy
// as one that doesn't exist: a find returns nil, and a write that changes no row is ErrNotFound.
type Repository interface {
	// Create stores the proxy in the workspace, whatever workspace the proxy says
	Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
}
