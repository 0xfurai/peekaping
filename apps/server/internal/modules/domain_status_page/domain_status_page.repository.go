package domain_status_page

import "context"

type Repository interface {
	Create(ctx context.Context, entity *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error

	// Additional methods for managing relationships
	AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error)
	RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error
	GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error)
	FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error)
	DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error
	FindByDomain(ctx context.Context, domain string) (*Model, error)
}
