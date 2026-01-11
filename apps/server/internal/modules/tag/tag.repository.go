package tag

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, entity *Model) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *Model) error
	UpdatePartial(ctx context.Context, id string, entity *UpdateModel) error
	Delete(ctx context.Context, id string, orgID string) error
	FindByName(ctx context.Context, name string, orgID string) (*Model, error)
}
