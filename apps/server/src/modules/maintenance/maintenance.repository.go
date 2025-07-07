package maintenance

import "context"

type Repository interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, strategy string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, id string) error

	SetActive(ctx context.Context, id string, active bool) (*Model, error)
	GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
}
