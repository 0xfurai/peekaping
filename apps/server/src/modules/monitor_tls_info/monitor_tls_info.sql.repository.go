package monitor_tls_info

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:monitor_tls_info,alias:mti"`

	ID        string    `bun:"id,pk"`
	MonitorID string    `bun:"monitor_id,notnull,unique"`
	InfoJSON  string    `bun:"info_json,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:        sm.ID,
		MonitorID: sm.MonitorID,
		InfoJSON:  sm.InfoJSON,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) GetByMonitorID(ctx context.Context, monitorID string) (*Model, error) {
	var sm sqlModel
	err := r.db.NewSelect().
		Model(&sm).
		Where("monitor_id = ?", monitorID).
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(&sm), nil
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, dto *CreateDto) (*Model, error) {
	sm := &sqlModel{
		ID:        uuid.New().String(),
		MonitorID: dto.MonitorID,
		InfoJSON:  dto.InfoJSON,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := r.db.NewInsert().
		Model(sm).
		Exec(ctx)

	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, monitorID string, dto *UpdateDto) (*Model, error) {
	sm := &sqlModel{
		InfoJSON:  dto.InfoJSON,
		UpdatedAt: time.Now(),
	}

	_, err := r.db.NewUpdate().
		Model(sm).
		Column("info_json", "updated_at").
		Where("monitor_id = ?", monitorID).
		Exec(ctx)

	if err != nil {
		return nil, err
	}

	// Fetch the updated record
	return r.GetByMonitorID(ctx, monitorID)
}

func (r *SQLRepositoryImpl) Upsert(ctx context.Context, monitorID string, infoJSON string) (*Model, error) {
	sm := &sqlModel{
		ID:        uuid.New().String(),
		MonitorID: monitorID,
		InfoJSON:  infoJSON,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := r.db.NewInsert().
		Model(sm).
		On("CONFLICT (monitor_id) DO UPDATE").
		Set("info_json = EXCLUDED.info_json").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)

	if err != nil {
		return nil, err
	}

	return r.GetByMonitorID(ctx, monitorID)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, monitorID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("monitor_id = ?", monitorID).
		Exec(ctx)

	return err
}

func (r *SQLRepositoryImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("updated_at < ?", cutoffDate).
		Exec(ctx)

	return err
}
