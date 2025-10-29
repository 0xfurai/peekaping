package notification_sent_history

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:notification_sent_history,alias:nsh"`

	ID        string    `bun:"id,pk"`
	Type      string    `bun:"type,notnull"`
	MonitorID string    `bun:"monitor_id,notnull"`
	Days      int       `bun:"days,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:        sm.ID,
		Type:      sm.Type,
		MonitorID: sm.MonitorID,
		Days:      sm.Days,
		CreatedAt: sm.CreatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) CheckIfSent(ctx context.Context, notificationType string, monitorID string, days int) (bool, error) {
	count, err := r.db.NewSelect().
		Model((*sqlModel)(nil)).
		Where("type = ? AND monitor_id = ? AND days >= ?", notificationType, monitorID, days).
		Count(ctx)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *SQLRepositoryImpl) RecordSent(ctx context.Context, dto *CreateDto) error {
	sm := &sqlModel{
		ID:        uuid.New().String(),
		Type:      dto.Type,
		MonitorID: dto.MonitorID,
		Days:      dto.Days,
		CreatedAt: time.Now(),
	}

	// Use INSERT ... ON CONFLICT DO NOTHING to handle duplicates gracefully
	_, err := r.db.NewInsert().
		Model(sm).
		On("CONFLICT (type, monitor_id, days) DO NOTHING").
		Exec(ctx)

	return err
}

func (r *SQLRepositoryImpl) ClearByMonitorAndType(ctx context.Context, monitorID string, notificationType string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("monitor_id = ? AND type = ?", monitorID, notificationType).
		Exec(ctx)

	return err
}

func (r *SQLRepositoryImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("created_at < ?", cutoffDate).
		Exec(ctx)

	return err
}

func (r *SQLRepositoryImpl) GetByMonitorAndType(ctx context.Context, monitorID string, notificationType string) ([]*Model, error) {
	var sqlModels []*sqlModel

	err := r.db.NewSelect().
		Model(&sqlModels).
		Where("monitor_id = ? AND type = ?", monitorID, notificationType).
		Order("days ASC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sqlModels {
		models = append(models, toDomainModelFromSQL(sm))
	}

	return models, nil
}
