package domain_status_page

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:domain_status_page,alias:dsp"`

	ID           string    `bun:"id,pk"`
	StatusPageID string    `bun:"status_page_id,notnull"`
	Domain       string    `bun:"domain,unique,notnull"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModel(sm *sqlModel) *Model {
	return &Model{
		ID:           sm.ID,
		StatusPageID: sm.StatusPageID,
		Domain:       sm.Domain,
		CreatedAt:    sm.CreatedAt,
		UpdatedAt:    sm.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	sm := &sqlModel{
		ID:           uuid.New().String(),
		StatusPageID: entity.StatusPageID,
		Domain:       entity.Domain,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

	if q != "" {
		query = query.Where("domain ILIKE ?", "%"+q+"%")
	}

	query = query.Order("created_at DESC").
		Limit(limit).
		Offset(page * limit)

	var sms []*sqlModel
	err := query.Scan(ctx, &sms)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModel(sm))
	}
	return models, nil
}

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	sm := &sqlModel{
		ID:           id,
		StatusPageID: entity.StatusPageID,
		Domain:       entity.Domain,
		UpdatedAt:    time.Now(),
	}

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		OmitZero().
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

	query = query.Set("updated_at = ?", time.Now())

	if entity.StatusPageID != nil {
		query = query.Set("status_page_id = ?", *entity.StatusPageID)
	}
	if entity.Domain != nil {
		query = query.Set("domain = ?", *entity.Domain)
	}

	_, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

// Additional methods for managing relationships
func (r *SQLRepositoryImpl) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error) {
	// Check if the relationship already exists
	existing, err := r.FindByStatusPageAndDomain(ctx, statusPageID, domain)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Update existing relationship
		return r.UpdatePartial(ctx, existing.ID, &PartialUpdateDto{})
	}

	// Create new relationship
	entity := &CreateUpdateDto{
		StatusPageID: statusPageID,
		Domain:       domain,
	}

	return r.Create(ctx, entity)
}

func (r *SQLRepositoryImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ? AND domain = ?", statusPageID, domain).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("status_page_id = ?", statusPageID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModel(sm))
	}
	return models, nil
}

func (r *SQLRepositoryImpl) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().
		Model(sm).
		Where("status_page_id = ? AND domain = ?", statusPageID, domain).
		Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ?", statusPageID).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().
		Model(sm).
		Where("domain = ?", domain).
		Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(sm), nil
}
