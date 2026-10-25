package stats

import (
	"context"
	"database/sql"
	"math"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/shared"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// sqlModel is a row of stat_minutely, stat_hourly or stat_daily; each query names its table.
type sqlModel struct {
	bun.BaseModel `bun:"alias:s"`

	MonitorID   string    `bun:"monitor_id,pk"`
	Timestamp   time.Time `bun:"timestamp,pk"`
	ProbeID     string    `bun:"probe_id,pk"`
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
		ProbeID:     sm.ProbeID,
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
		ProbeID:     s.ProbeID,
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

// statTable returns the table expression for period
func statTable(period StatPeriod) string {
	table, ok := statTables[period]
	if !ok {
		table = statTables[StatMinutely]
	}
	return table + " AS s"
}

// AddToStat adds stat's counts and response times to the row for its monitor, probe and
// timestamp, and creates the row if there isn't one, in one statement. Concurrent
// heartbeats in the same bucket therefore can't overwrite each other's counts.
func (r *SQLRepositoryImpl) AddToStat(ctx context.Context, stat *Stat, period StatPeriod) error {
	sm := toSQLModel(stat)
	if sm.ProbeID == "" {
		sm.ProbeID = shared.LocalProbeID
	}
	_, err := r.db.NewInsert().
		Model(sm).
		ModelTableExpr(statTable(period)).
		On("CONFLICT (monitor_id, timestamp, probe_id) DO UPDATE").
		Set("up = s.up + EXCLUDED.up").
		Set("down = s.down + EXCLUDED.down").
		Set("maintenance = s.maintenance + EXCLUDED.maintenance").
		Set("ping_sum = s.ping_sum + EXCLUDED.ping_sum").
		Set("ping_count = s.ping_count + EXCLUDED.ping_count").
		// ping_min and ping_max are NULL until the row has a response time, and a
		// comparison with NULL is never true, so a NULL never replaces a value.
		Set("ping_min = CASE WHEN EXCLUDED.ping_min < s.ping_min OR s.ping_min IS NULL THEN EXCLUDED.ping_min ELSE s.ping_min END").
		Set("ping_max = CASE WHEN EXCLUDED.ping_max > s.ping_max OR s.ping_max IS NULL THEN EXCLUDED.ping_max ELSE s.ping_max END").
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod) ([]*Stat, error) {
	// A bucket holds a row for each probe that ran checks in it. Without a probe the monitor's
	// figures add them up; with one there is a single row to add up.
	var sms []*sqlModel
	query := r.db.NewSelect().
		Model(&sms).
		ModelTableExpr(statTable(period)).
		ColumnExpr("timestamp").
		ColumnExpr("SUM(up) AS up").
		ColumnExpr("SUM(down) AS down").
		ColumnExpr("SUM(maintenance) AS maintenance").
		ColumnExpr("SUM(ping_sum) AS ping_sum").
		ColumnExpr("SUM(ping_count) AS ping_count").
		ColumnExpr("MIN(ping_min) AS ping_min").
		ColumnExpr("MAX(ping_max) AS ping_max").
		Where("monitor_id = ? AND timestamp BETWEEN ? AND ?", monitorID, since, until).
		GroupExpr("timestamp").
		Order("timestamp ASC")
	if probeID != "" {
		query = query.Where("probe_id = ?", probeID)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	for _, sm := range sms {
		sm.MonitorID, sm.ProbeID = monitorID, probeID
	}

	var stats []*Stat
	for _, sm := range sms {
		stats = append(stats, toDomainModelFromSQL(sm))
	}
	return stats, nil
}

// deleteBatchSize is how many rows one DELETE removes, so a cleanup with a large
// backlog never holds SQLite's single connection for long.
const deleteBatchSize = 5000

func (r *SQLRepositoryImpl) DeleteOlderThan(ctx context.Context, period StatPeriod, cutoff time.Time) (int64, error) {
	if r.db.Dialect().Name() == dialect.SQLite {
		return r.deleteOlderThanByRowid(ctx, period, cutoff)
	}
	var deleted int64
	for {
		batch := r.db.NewSelect().
			Model((*sqlModel)(nil)).
			ModelTableExpr(statTable(period)).
			Column("monitor_id", "timestamp", "probe_id").
			Where("timestamp < ?", cutoff).
			Limit(deleteBatchSize)
		result, err := r.db.NewDelete().
			Model((*sqlModel)(nil)).
			ModelTableExpr(statTable(period)).
			Where("(monitor_id, timestamp, probe_id) IN (?)", batch).
			Exec(ctx)
		if err != nil {
			return deleted, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += n
		if n < deleteBatchSize {
			return deleted, nil
		}
	}
}

// deleteOlderThanByRowid walks SQLite's rowids in windows of deleteBatchSize, so each
// statement reads and deletes at most that many rows. SQLite keeps rows in rowid
// order, which puts a window's rows on a few neighbouring pages. Deleting one
// monitor's oldest rows at a time instead touches a page per row, because each
// minute's rows for all monitors are written side by side, and holds the single
// connection for about a second per batch.
func (r *SQLRepositoryImpl) deleteOlderThanByRowid(ctx context.Context, period StatPeriod, cutoff time.Time) (int64, error) {
	var deleted int64
	var next sql.NullInt64
	from := int64(math.MinInt64)
	for {
		err := r.db.NewSelect().
			Model((*sqlModel)(nil)).
			ModelTableExpr(statTable(period)).
			ColumnExpr("min(rowid)").
			Where("rowid >= ?", from).
			Scan(ctx, &next)
		if err != nil || !next.Valid {
			return deleted, err
		}
		from = next.Int64 + deleteBatchSize
		result, err := r.db.NewDelete().
			Model((*sqlModel)(nil)).
			ModelTableExpr(statTable(period)).
			Where("rowid >= ? AND rowid < ? AND timestamp < ?", next.Int64, from, cutoff).
			Exec(ctx)
		if err != nil {
			return deleted, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += n
	}
}

func (r *SQLRepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	for period := range statTables {
		_, err := dbtx.DB(ctx, r.db).NewDelete().
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
