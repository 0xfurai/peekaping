package monitor_status_page

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:monitor_status_pages,alias:msp"`

	ID           string    `bun:"id,pk"`
	MonitorID    string    `bun:"monitor_id,notnull"`
	StatusPageID string    `bun:"status_page_id,notnull"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:           sm.ID,
		MonitorID:    sm.MonitorID,
		StatusPageID: sm.StatusPageID,
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
		MonitorID:    entity.MonitorID,
		StatusPageID: entity.StatusPageID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
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
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

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
		models = append(models, toDomainModelFromSQL(sm))
	}
	return models, nil
}

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	sm := &sqlModel{
		ID:           id,
		MonitorID:    entity.MonitorID,
		StatusPageID: entity.StatusPageID,
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

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

	hasUpdates := false

	if entity.MonitorID != nil {
		query = query.Set("monitor_id = ?", *entity.MonitorID)
		hasUpdates = true
	}
	if entity.StatusPageID != nil {
		query = query.Set("status_page_id = ?", *entity.StatusPageID)
		hasUpdates = true
	}

	if !hasUpdates {
		return r.FindByID(ctx, id)
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

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

func (r *SQLRepositoryImpl) AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*Model, error) {
	sm := &sqlModel{
		ID:           uuid.New().String(),
		MonitorID:    monitorID,
		StatusPageID: statusPageID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ? AND monitor_id = ?", statusPageID, monitorID).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("status_page_id = ?", statusPageID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModelFromSQL(sm))
	}
	return models, nil
}

func (r *SQLRepositoryImpl) FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().
		Model(sm).
		Where("status_page_id = ? AND monitor_id = ?", statusPageID, monitorID).
		Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*Model, error) {
	// For this simple implementation, we'll just update the updated_at timestamp
	// In a real implementation, you'd have an order field
	_, err := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Set("updated_at = ?", time.Now()).
		Where("status_page_id = ? AND monitor_id = ?", statusPageID, monitorID).
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByStatusPageAndMonitor(ctx, statusPageID, monitorID)
}

func (r *SQLRepositoryImpl) UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*Model, error) {
	// For this simple implementation, we'll just update the updated_at timestamp
	// In a real implementation, you'd have an active field
	_, err := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Set("updated_at = ?", time.Now()).
		Where("status_page_id = ? AND monitor_id = ?", statusPageID, monitorID).
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByStatusPageAndMonitor(ctx, statusPageID, monitorID)
}

func (r *SQLRepositoryImpl) DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("status_page_id = ?", statusPageID).
		Exec(ctx)
	return err
}
