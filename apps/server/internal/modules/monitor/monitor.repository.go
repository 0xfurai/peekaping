package monitor

import "context"

type MonitorRepository interface {
	Create(ctx context.Context, monitor *Model) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindByIDs(ctx context.Context, ids []string, orgID string) ([]*Model, error)
	FindAll(
		ctx context.Context,
		page int,
		limit int,
		q string,
		active *bool,
		status *int,
		tagIds []string,
		orgID string,
	) ([]*Model, error)
	FindActive(ctx context.Context) ([]*Model, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, monitor *Model, orgID string) error
	UpdatePartial(ctx context.Context, id string, monitor *UpdateModel, orgID string) error
	Delete(ctx context.Context, id string, orgID string) error
	RemoveProxyReference(ctx context.Context, proxyId string) error
	FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error)
	FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error)
}
