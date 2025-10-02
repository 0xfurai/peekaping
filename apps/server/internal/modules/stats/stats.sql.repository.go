package stats

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:stats,alias:s"`

	ID          string    `bun:"id,pk"`
	MonitorID   string    `bun:"monitor_id,notnull"`
	Timestamp   time.Time `bun:"timestamp,notnull"`
	Ping        float64   `bun:"ping,notnull,default:0"`
	PingMin     float64   `bun:"ping_min,notnull,default:0"`
	PingMax     float64   `bun:"ping_max,notnull,default:0"`
	Up          int       `bun:"up,notnull,default:0"`
	Down        int       `bun:"down,notnull,default:0"`
	Maintenance int       `bun:"maintenance,notnull,default:0"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Stat {
	return &Stat{
		ID:          sm.ID,
		MonitorID:   sm.MonitorID,
		Timestamp:   sm.Timestamp,
		Ping:        sm.Ping,
		PingMin:     sm.PingMin,
		PingMax:     sm.PingMax,
		Up:          sm.Up,
		Down:        sm.Down,
		Maintenance: sm.Maintenance,
	}
}

func toSQLModel(s *Stat) *sqlModel {
	return &sqlModel{
		ID:          s.ID,
		MonitorID:   s.MonitorID,
		Timestamp:   s.Timestamp,
		Ping:        s.Ping,
		PingMin:     s.PingMin,
		PingMax:     s.PingMax,
		Up:          s.Up,
		Down:        s.Down,
		Maintenance: s.Maintenance,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) GetOrCreateStat(ctx context.Context, monitorID string, timestamp time.Time, period StatPeriod) (*Stat, error) {
	sm := new(sqlModel)

	// Try to find existing stat
	err := r.db.NewSelect().
		Model(sm).
		Where("monitor_id = ? AND timestamp = ?", monitorID, timestamp).
		Scan(ctx)

	if err != nil && err.Error() == "sql: no rows in result set" {
		// Create new stat if not found
		sm = &sqlModel{
			ID:          uuid.New().String(),
			MonitorID:   monitorID,
			Timestamp:   timestamp,
			Ping:        0,
			PingMin:     0,
			PingMax:     0,
			Up:          0,
			Down:        0,
			Maintenance: 0,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		_, err = r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) UpsertStat(ctx context.Context, stat *Stat, period StatPeriod) error {
	sm := toSQLModel(stat)
	sm.UpdatedAt = time.Now()

	// Try to update existing record first
	result, err := r.db.NewUpdate().
		Model(sm).
		Where("monitor_id = ? AND timestamp = ?", sm.MonitorID, sm.Timestamp).
		Set("ping = ?", sm.Ping).
		Set("ping_min = ?", sm.PingMin).
		Set("ping_max = ?", sm.PingMax).
		Set("up = ?", sm.Up).
		Set("down = ?", sm.Down).
		Set("maintenance = ?", sm.Maintenance).
		Set("updated_at = ?", sm.UpdatedAt).
		Exec(ctx)

	if err != nil {
		return err
	}

	// Check if any rows were affected by the update
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// If no rows were updated, insert a new record
	if rowsAffected == 0 {
		// Generate new ID for insert
		if sm.ID == "" {
			sm.ID = uuid.New().String()
		}
		sm.CreatedAt = time.Now()

		_, err = r.db.NewInsert().Model(sm).Exec(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *SQLRepositoryImpl) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod) ([]*Stat, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("monitor_id = ? AND timestamp BETWEEN ? AND ?", monitorID, since, until).
		Order("timestamp ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var stats []*Stat
	for _, sm := range sms {
		stats = append(stats, toDomainModelFromSQL(sm))
	}
	return stats, nil
}

func (r *SQLRepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("monitor_id = ?", monitorID).
		Exec(ctx)
	return err
}
