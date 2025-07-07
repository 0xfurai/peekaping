package heartbeat

import (
	"context"
	"time"

	"peekaping/src/modules/shared"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:heartbeats,alias:h"`

	ID        string    `bun:"id,pk"`
	MonitorID string    `bun:"monitor_id,notnull"`
	Status    int       `bun:"status,notnull"`
	Msg       string    `bun:"msg"`
	Ping      int       `bun:"ping"`
	Duration  int       `bun:"duration"`
	DownCount int       `bun:"down_count"`
	Retries   int       `bun:"retries"`
	Important bool      `bun:"important,notnull,default:false"`
	Time      time.Time `bun:"time,nullzero,notnull,default:current_timestamp"`
	EndTime   time.Time `bun:"end_time,nullzero"`
	Notified  bool      `bun:"notified,notnull,default:false"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:        sm.ID,
		MonitorID: sm.MonitorID,
		Status:    MonitorStatus(sm.Status),
		Msg:       sm.Msg,
		Ping:      sm.Ping,
		Duration:  sm.Duration,
		DownCount: sm.DownCount,
		Retries:   sm.Retries,
		Important: sm.Important,
		Time:      sm.Time,
		EndTime:   sm.EndTime,
		Notified:  sm.Notified,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:        m.ID,
		MonitorID: m.MonitorID,
		Status:    int(m.Status),
		Msg:       m.Msg,
		Ping:      m.Ping,
		Duration:  m.Duration,
		DownCount: m.DownCount,
		Retries:   m.Retries,
		Important: m.Important,
		Time:      m.Time,
		EndTime:   m.EndTime,
		Notified:  m.Notified,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, heartbeat *Model) (*Model, error) {
	sm := toSQLModel(heartbeat)
	sm.ID = uuid.New().String()
	sm.Time = time.Now()

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

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Order("time DESC").
		Limit(limit).
		Offset((page - 1) * limit).
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

func (r *SQLRepositoryImpl) FindActive(ctx context.Context) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("status = ?", int(shared.MonitorStatusUp)).
		Order("time DESC").
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

func (r *SQLRepositoryImpl) FindByMonitorIDPaginated(
	ctx context.Context,
	monitorID string,
	limit int,
	page int,
	important *bool,
	reverse bool,
) ([]*Model, error) {
	query := r.db.NewSelect().
		Model((*sqlModel)(nil)).
		Where("monitor_id = ?", monitorID).
		Limit(limit).
		Offset((page - 1) * limit)

	if important != nil {
		query = query.Where("important = ?", *important)
	}

	query = query.Order("time DESC")

	var sms []*sqlModel
	err := query.Scan(ctx, &sms)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModelFromSQL(sm))
	}

	if reverse && len(models) > 1 {
		for i, j := 0, len(models)-1; i < j; i, j = i+1, j-1 {
			models[i], models[j] = models[j], models[i]
		}
	}

	return models, nil
}

func (r *SQLRepositoryImpl) FindUptimeStatsByMonitorID(
	ctx context.Context,
	monitorID string,
	periods map[string]time.Duration,
	now time.Time,
) (map[string]float64, error) {
	stats := make(map[string]float64)

	for name, duration := range periods {
		since := now.Add(-duration)

		var result struct {
			Total int `bun:"total"`
			Up    int `bun:"up"`
		}

		err := r.db.NewSelect().
			Model((*sqlModel)(nil)).
			ColumnExpr("COUNT(*) as total").
			ColumnExpr("COUNT(CASE WHEN status = ? THEN 1 END) as up", 1).
			Where("monitor_id = ? AND time >= ?", monitorID, since).
			Scan(ctx, &result)

		if err != nil {
			return nil, err
		}

		if result.Total > 0 {
			stats[name] = float64(result.Up) / float64(result.Total) * 100
		} else {
			stats[name] = 0
		}
	}

	return stats, nil
}

func (r *SQLRepositoryImpl) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("time < ?", cutoff).
		Exec(ctx)
	if err != nil {
		return 0, err
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

func (r *SQLRepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("monitor_id = ?", monitorID).
		Exec(ctx)
	return err
}
