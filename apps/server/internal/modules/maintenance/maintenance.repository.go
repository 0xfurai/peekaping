package maintenance

import "context"

type Repository interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error

	SetActive(ctx context.Context, id string, active bool, orgID string) (*Model, error)
	GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
}
