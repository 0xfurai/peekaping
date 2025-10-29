package proxy

import "context"

type Repository interface {
	Create(ctx context.Context, entity *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error
}
