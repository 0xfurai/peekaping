package monitor_notification

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:monitor_notifications,alias:mn"`

	ID                    string    `bun:"id,pk"`
	MonitorID             string    `bun:"monitor_id,notnull"`
	NotificationChannelID string    `bun:"notification_channel_id,notnull"`
	CreatedAt             time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt             time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:             sm.ID,
		MonitorID:      sm.MonitorID,
		NotificationID: sm.NotificationChannelID,
		CreatedAt:      sm.CreatedAt,
		UpdatedAt:      sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:                    m.ID,
		MonitorID:             m.MonitorID,
		NotificationChannelID: m.NotificationID,
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, model *Model) (*Model, error) {
	sm := toSQLModel(model)
	sm.ID = uuid.New().String()
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

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

func (r *SQLRepositoryImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("monitor_id = ?", monitorID).
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

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("monitor_id = ?", monitorID).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("notification_channel_id = ?", notificationID).Exec(ctx)
	return err
}
