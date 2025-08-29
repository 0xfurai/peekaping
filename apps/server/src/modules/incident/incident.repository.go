package incident

import "context"

type Repository interface {
	Create(ctx context.Context, incident *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error)
	Update(ctx context.Context, id string, incident *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error
}
