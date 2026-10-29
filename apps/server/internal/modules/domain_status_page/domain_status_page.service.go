package domain_status_page

import (
	"context"
	"fmt"
	"strings"

	"peekaping/internal/dbtx"

	"go.uber.org/zap"
)

// Normalize is the form a custom domain is stored in and looked up by: a host name is the same in
// any case, and with spaces around it. The service normalizes every domain it is given, so a
// caller that compares domains itself should too.
func Normalize(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// DomainTakenError is a domain that another status page holds. Custom domains are unique across the
// install, and the database enforces it, so this is also what two pages that race for a domain get.
type DomainTakenError struct {
	Domain string
}

func (e *DomainTakenError) Error() string {
	return fmt.Sprintf("domain %q is used by another status page", e.Domain)
}

// refuseTaken is err, or a DomainTakenError for the domain when err is the database refusing a
// domain that is held already
func refuseTaken(err error, domain string) error {
	if dbtx.IsUniqueViolation(err) {
		return &DomainTakenError{Domain: domain}
	}
	return err
}

type Service interface {
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

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		logger.Named("[domain-status-page-service]"),
	}
}

func (dr *ServiceImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	normalized := *entity
	normalized.Domain = Normalize(entity.Domain)
	created, err := dr.repository.Create(ctx, &normalized)
	return created, refuseTaken(err, normalized.Domain)
}

func (dr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return dr.repository.FindByID(ctx, id)
}

func (dr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return dr.repository.FindAll(ctx, page, limit, q)
}

func (dr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error) {
	normalized := *entity
	normalized.Domain = Normalize(entity.Domain)
	updated, err := dr.repository.UpdateFull(ctx, id, &normalized)
	return updated, refuseTaken(err, normalized.Domain)
}

func (dr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error) {
	normalized := *entity
	var domain string
	if entity.Domain != nil {
		domain = Normalize(*entity.Domain)
		normalized.Domain = &domain
	}
	updated, err := dr.repository.UpdatePartial(ctx, id, &normalized)
	return updated, refuseTaken(err, domain)
}

func (dr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return dr.repository.Delete(ctx, id)
}

func (dr *ServiceImpl) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error) {
	domain = Normalize(domain)
	dr.logger.Debugw("Adding domain to status page", "statusPageID", statusPageID, "domain", domain)
	added, err := dr.repository.AddDomainToStatusPage(ctx, statusPageID, domain)
	return added, refuseTaken(err, domain)
}

func (dr *ServiceImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	return dr.repository.RemoveDomainFromStatusPage(ctx, statusPageID, Normalize(domain))
}

func (dr *ServiceImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	return dr.repository.GetDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error) {
	return dr.repository.FindByStatusPageAndDomain(ctx, statusPageID, Normalize(domain))
}

func (dr *ServiceImpl) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	return dr.repository.DeleteAllDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	return dr.repository.FindByDomain(ctx, Normalize(domain))
}
