package domain_status_page

import (
	"context"
	"time"
)

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

	// SetTokenIfEmpty gives a row without a verification token the one passed, and returns the row
	// with the token it now has
	SetTokenIfEmpty(ctx context.Context, id, token string) (*Model, error)
	// SaveVerification stores a check's result: the four times of the model
	SaveVerification(ctx context.Context, m *Model) error
	// FindDueAnyWorkspace lists the domains a check is due for in every workspace: the Pending ones,
	// and the Verified ones last checked before verifiedBefore or never
	FindDueAnyWorkspace(ctx context.Context, verifiedBefore time.Time) ([]*Model, error)
}
