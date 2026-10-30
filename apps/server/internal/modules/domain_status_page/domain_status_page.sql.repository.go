package domain_status_page

import (
	"context"
	"time"

	"peekaping/internal/dbtx"

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

	VerificationToken string     `bun:"verification_token,notnull"`
	VerifiedAt        *time.Time `bun:"verified_at"`
	PendingSince      *time.Time `bun:"pending_since"`
	FailingSince      *time.Time `bun:"failing_since"`
	LastCheckedAt     *time.Time `bun:"last_checked_at"`
}

func toDomainModel(sm *sqlModel) *Model {
	return &Model{
		ID:           sm.ID,
		StatusPageID: sm.StatusPageID,
		Domain:       sm.Domain,
		CreatedAt:    sm.CreatedAt,
		UpdatedAt:    sm.UpdatedAt,

		VerificationToken: sm.VerificationToken,
		VerifiedAt:        sm.VerifiedAt,
		PendingSince:      sm.PendingSince,
		FailingSince:      sm.FailingSince,
		LastCheckedAt:     sm.LastCheckedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	sm := &sqlModel{
		ID:           uuid.New().String(),
		StatusPageID: entity.StatusPageID,
		Domain:       entity.Domain,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),

		VerificationToken: entity.VerificationToken,
	}
	if sm.VerificationToken == "" {
		token, err := NewToken()
		if err != nil {
			return nil, err
		}
		sm.VerificationToken = token
	}

	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	query := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil))

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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error) {
	sm := &sqlModel{
		ID:           id,
		StatusPageID: entity.StatusPageID,
		Domain:       entity.Domain,
		UpdatedAt:    time.Now(),
	}

	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model(sm).
		Where("id = ?", id).
		OmitZero().
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error) {
	query := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

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
	_, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
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
		return r.UpdatePartial(ctx, existing.ID, &UpdateModel{})
	}

	// Create new relationship
	entity := &Model{
		StatusPageID: statusPageID,
		Domain:       domain,
	}

	return r.Create(ctx, entity)
}

func (r *SQLRepositoryImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ? AND domain = ?", statusPageID, domain).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
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
	err := dbtx.DB(ctx, r.db).NewSelect().
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
	_, err := dbtx.DB(ctx, r.db).NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ?", statusPageID).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().
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

// SetTokenIfEmpty gives a row that has no verification token the one passed, and leaves a row that
// has one alone, so that two callers racing to give a row its token agree on the winner's
func (r *SQLRepositoryImpl) SetTokenIfEmpty(ctx context.Context, id, token string) (*Model, error) {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model((*sqlModel)(nil)).
		Set("verification_token = ?", token).
		Where("id = ? AND verification_token = ''", id).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

// SaveVerification stores the state of a check: the four times, and nothing else of the row. A row
// that is gone meanwhile matches nothing.
func (r *SQLRepositoryImpl) SaveVerification(ctx context.Context, m *Model) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model((*sqlModel)(nil)).
		Set("verified_at = ?", m.VerifiedAt).
		Set("pending_since = ?", m.PendingSince).
		Set("failing_since = ?", m.FailingSince).
		Set("last_checked_at = ?", m.LastCheckedAt).
		Where("id = ?", m.ID).
		Exec(ctx)
	return err
}

// FindDueAnyWorkspace lists the domains of every workspace that a check is due for: each Pending
// one, and each Verified one last checked before verifiedBefore or never
func (r *SQLRepositoryImpl) FindDueAnyWorkspace(ctx context.Context, verifiedBefore time.Time) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("verified_at IS NULL OR last_checked_at IS NULL OR last_checked_at < ?", verifiedBefore).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]*Model, len(sms))
	for i, sm := range sms {
		models[i] = toDomainModel(sm)
	}
	return models, nil
}
