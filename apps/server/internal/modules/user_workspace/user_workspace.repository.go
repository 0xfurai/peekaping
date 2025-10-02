package user_workspace

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, userWorkspace *Model) (*Model, error)
	FindByUserID(ctx context.Context, userID string) ([]*Model, error)
	FindByWorkspaceID(ctx context.Context, workspaceID string) ([]*Model, error)
	FindByUserAndWorkspace(ctx context.Context, userID, workspaceID string) (*Model, error)
	Update(ctx context.Context, userID, workspaceID string, userWorkspace *UpdateModel) error
	Delete(ctx context.Context, userID, workspaceID string) error
}
