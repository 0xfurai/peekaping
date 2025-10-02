package domain_status_page

import (
	"context"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
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

func (dr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	return dr.repository.Create(ctx, entity)
}

func (dr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return dr.repository.FindByID(ctx, id)
}

func (dr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return dr.repository.FindAll(ctx, page, limit, q)
}

func (dr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	return dr.repository.UpdateFull(ctx, id, entity)
}

func (dr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	return dr.repository.UpdatePartial(ctx, id, entity)
}

func (dr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return dr.repository.Delete(ctx, id)
}

func (dr *ServiceImpl) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error) {
	dr.logger.Debugw("Adding domain to status page", "statusPageID", statusPageID, "domain", domain)
	return dr.repository.AddDomainToStatusPage(ctx, statusPageID, domain)
}

func (dr *ServiceImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	return dr.repository.RemoveDomainFromStatusPage(ctx, statusPageID, domain)
}

func (dr *ServiceImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	return dr.repository.GetDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error) {
	return dr.repository.FindByStatusPageAndDomain(ctx, statusPageID, domain)
}

func (dr *ServiceImpl) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	return dr.repository.DeleteAllDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	return dr.repository.FindByDomain(ctx, domain)
}
