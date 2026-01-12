package status_page

import "context"

type Repository interface {
	Create(ctx context.Context, statusPage *Model) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindBySlug(ctx context.Context, slug string) (*Model, error)
	FindAll(
		ctx context.Context,
		page int,
		limit int,
		q string,
		orgID string,
	) ([]*Model, error)
	Update(ctx context.Context, id string, statusPage *UpdateModel, orgID string) error
	Delete(ctx context.Context, id string, orgID string) error
}
