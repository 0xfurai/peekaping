package stats

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// sqlModel is a row of stat_minutely, stat_hourly or stat_daily; each query names its table.
type sqlModel struct {
	bun.BaseModel `bun:"alias:s"`

	MonitorID   string    `bun:"monitor_id,pk"`
	Timestamp   time.Time `bun:"timestamp,pk"`
	Up          int       `bun:"up,notnull"`
	Down        int       `bun:"down,notnull"`
	Maintenance int       `bun:"maintenance,notnull"`
	PingSum     float64   `bun:"ping_sum,notnull"`
	PingCount   int       `bun:"ping_count,notnull"`
	PingMin     *float64  `bun:"ping_min"`
	PingMax     *float64  `bun:"ping_max"`
}

func toDomainModelFromSQL(sm *sqlModel) *Stat {
	s := &Stat{
		MonitorID:   sm.MonitorID,
		Timestamp:   sm.Timestamp,
		PingCount:   sm.PingCount,
		Up:          sm.Up,
		Down:        sm.Down,
		Maintenance: sm.Maintenance,
	}
	if sm.PingCount > 0 {
		s.Ping = sm.PingSum / float64(sm.PingCount)
	}
	if sm.PingMin != nil {
		s.PingMin = *sm.PingMin
	}
	if sm.PingMax != nil {
		s.PingMax = *sm.PingMax
	}
	return s
}

func toSQLModel(s *Stat) *sqlModel {
	sm := &sqlModel{
		MonitorID:   s.MonitorID,
		Timestamp:   s.Timestamp,
		Up:          s.Up,
		Down:        s.Down,
		Maintenance: s.Maintenance,
		PingSum:     s.Ping * float64(s.PingCount),
		PingCount:   s.PingCount,
	}
	if s.PingCount > 0 {
		sm.PingMin, sm.PingMax = &s.PingMin, &s.PingMax
	}
	return sm
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

var statTables = map[StatPeriod]string{
	StatMinutely: "stat_minutely",
	StatHourly:   "stat_hourly",
	StatDaily:    "stat_daily",
}

// statTable returns the table expression for period, like the Mongo repository's getStatCollection.
func statTable(period StatPeriod) string {
	table, ok := statTables[period]
	if !ok {
		table = statTables[StatMinutely]
	}
	return table + " AS s"
}

func (r *SQLRepositoryImpl) GetOrCreateStat(ctx context.Context, monitorID string, timestamp time.Time, period StatPeriod) (*Stat, error) {
	sm := new(sqlModel)

	// Try to find existing stat
	err := r.db.NewSelect().
		Model(sm).
		ModelTableExpr(statTable(period)).
		Where("monitor_id = ? AND timestamp = ?", monitorID, timestamp).
		Scan(ctx)

	if errors.Is(err, sql.ErrNoRows) {
		// Create new stat if not found
		sm = &sqlModel{MonitorID: monitorID, Timestamp: timestamp}

		_, err = r.db.NewInsert().Model(sm).ModelTableExpr(statTable(period)).Exec(ctx)
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

	// Try to update existing record first
	result, err := r.db.NewUpdate().
		Model(sm).
		ModelTableExpr(statTable(period)).
		Where("monitor_id = ? AND timestamp = ?", sm.MonitorID, sm.Timestamp).
		Set("up = ?", sm.Up).
		Set("down = ?", sm.Down).
		Set("maintenance = ?", sm.Maintenance).
		Set("ping_sum = ?", sm.PingSum).
		Set("ping_count = ?", sm.PingCount).
		Set("ping_min = ?", sm.PingMin).
		Set("ping_max = ?", sm.PingMax).
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
		_, err = r.db.NewInsert().Model(sm).ModelTableExpr(statTable(period)).Exec(ctx)
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
		ModelTableExpr(statTable(period)).
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
	for period := range statTables {
		_, err := r.db.NewDelete().
			Model((*sqlModel)(nil)).
			ModelTableExpr(statTable(period)).
			Where("monitor_id = ?", monitorID).
			Exec(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
