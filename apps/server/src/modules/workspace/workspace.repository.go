package workspace

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, workspace *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByIDs(ctx context.Context, workspaceIDs []string) ([]*Model, error)
	FindAll(ctx context.Context, page int, limit int) ([]*Model, error)
	Update(ctx context.Context, id string, workspace *UpdateModel) error
	Delete(ctx context.Context, id string) error
}
