package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.uber.org/dig"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:monitors,alias:m"`

	ID string `bun:"id,pk"`
	// WorkspaceID is set when the monitor is created and never changes
	WorkspaceID    string `bun:"workspace_id,notnull"`
	Type           string `bun:"type,notnull"`
	Name           string `bun:"name,notnull"`
	Interval       int    `bun:"interval,notnull"`
	Timeout        int    `bun:"timeout,notnull"`
	MaxRetries     int    `bun:"max_retries,notnull"`
	RetryInterval  int    `bun:"retry_interval,notnull"`
	ResendInterval int    `bun:"resend_interval,notnull"`
	// LocationRule is any, all or at_least. LocationThreshold is set with at_least and NULL otherwise.
	LocationRule      string               `bun:"location_rule,notnull"`
	LocationThreshold *int                 `bun:"location_threshold"`
	Active            bool                 `bun:"active,notnull"` // no Bun default: it would replace false with the column default
	Status            shared.MonitorStatus `bun:"status,notnull,default:0"`
	CreatedAt         time.Time            `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time            `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	Config            string               `bun:"config"`
	ProxyId           *string              `bun:"proxy_id"`
	// PushToken is NULL for a monitor without one: the column is unique, which an empty string
	// would break for the second such monitor
	PushToken string `bun:"push_token,nullzero"`
	// LocationsStatus is the status the monitor's locations last added up to. Only the ingester's
	// RecordLocation writes it.
	LocationsStatus *int `bun:"locations_status,scanonly"`
}

// sqlProbeLink is the row that says a monitor runs on a probe. It is what is written when a
// monitor gets a probe; the state the row holds is written by RecordLocation alone.
type sqlProbeLink struct {
	bun.BaseModel `bun:"table:monitor_probes,alias:mp"`

	MonitorID string `bun:"monitor_id,pk"`
	ProbeID   string `bun:"probe_id,pk"`
}

// sqlLocation is a probe row of a monitor as it is read: with the state the row holds, and the
// name and status of the probe it names
type sqlLocation struct {
	bun.BaseModel `bun:"table:monitor_probes,alias:mp"`

	MonitorID       string     `bun:"monitor_id"`
	ProbeID         string     `bun:"probe_id"`
	Status          *int       `bun:"status"`
	LastPing        *int       `bun:"last_ping"`
	LastHeartbeatAt *time.Time `bun:"last_heartbeat_at"`
	ProbeName       string     `bun:"probe_name"`
	ProbeStatus     string     `bun:"probe_status"`
	MonitorType     string     `bun:"monitor_type"`
}

// probeOnline is the status the probes table keeps for a probe with an instance connected
const probeOnline = "online"

// online reports whether the probe the row names counts as online for the monitor. The local
// probe's checks run in the Peekaping process itself, whether or not any instance ever said hello,
// unless the install has no process of its own that runs checks (the cluster on SQLite and
// PostgreSQL): there its instances do, and it is online like a remote probe. A push monitor runs no
// checks, though: its pushes arrive at the api, so it needs no instance.
func (l *sqlLocation) online(local shared.LocalProbeLiveness) bool {
	if l.ProbeStatus == probeOnline || !local.FollowsItsStatus(l.ProbeID) {
		return true
	}
	return l.ProbeID == shared.LocalProbeID && l.MonitorType == pushType
}

func (l *sqlLocation) toLocation(local shared.LocalProbeLiveness) shared.MonitorLocation {
	location := shared.MonitorLocation{
		ProbeID:   l.ProbeID,
		ProbeName: l.ProbeName,
		Online:    l.online(local),
		LastPing:  l.LastPing,
	}
	if l.Status != nil {
		status := shared.MonitorStatus(*l.Status)
		location.Status = &status
	}
	if l.LastHeartbeatAt != nil {
		at := l.LastHeartbeatAt.UTC()
		location.LastHeartbeatAt = &at
	}
	return location
}

// localLocation is where a monitor without a probe row runs: on the local probe, which has not
// reported
func localLocation() shared.MonitorLocation {
	return shared.MonitorLocation{ProbeID: shared.LocalProbeID, ProbeName: "Local", Online: true}
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	// Handle nil ProxyId by converting to empty string
	var proxyId string
	if sm.ProxyId != nil {
		proxyId = *sm.ProxyId
	}

	return &Model{
		ID:                sm.ID,
		WorkspaceID:       workspace.ID(sm.WorkspaceID),
		Type:              sm.Type,
		Name:              sm.Name,
		Interval:          sm.Interval,
		Timeout:           sm.Timeout,
		MaxRetries:        sm.MaxRetries,
		RetryInterval:     sm.RetryInterval,
		ResendInterval:    sm.ResendInterval,
		LocationRule:      shared.LocationRule(sm.LocationRule),
		LocationThreshold: sm.LocationThreshold,
		Active:            sm.Active,
		Status:            sm.Status,
		CreatedAt:         sm.CreatedAt,
		UpdatedAt:         sm.UpdatedAt,
		Config:            sm.Config,
		ProxyId:           proxyId,
		PushToken:         sm.PushToken,
	}
}

// locationRuleOrAny is the rule, or any for a monitor that has none
func locationRuleOrAny(rule shared.LocationRule) shared.LocationRule {
	if rule == "" {
		return shared.LocationRuleAny
	}
	return rule
}

func toSQLModel(m *Model) *sqlModel {
	// Handle empty ProxyId by converting to NULL for PostgreSQL UUID compatibility
	var proxyId *string
	if m.ProxyId != "" {
		proxyId = &m.ProxyId
	}

	return &sqlModel{
		ID:                m.ID,
		WorkspaceID:       string(m.WorkspaceID),
		Type:              m.Type,
		Name:              m.Name,
		Interval:          m.Interval,
		Timeout:           m.Timeout,
		MaxRetries:        m.MaxRetries,
		RetryInterval:     m.RetryInterval,
		ResendInterval:    m.ResendInterval,
		LocationRule:      string(locationRuleOrAny(m.LocationRule)),
		LocationThreshold: m.LocationThreshold,
		Active:            m.Active,
		Status:            m.Status,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
		Config:            m.Config,
		ProxyId:           proxyId,
		PushToken:         m.PushToken,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
	// localProbe is how the local probe is known to be online
	localProbe shared.LocalProbeLiveness
}

// SQLRepositoryOption changes how the SQL repository behaves, for an install that has a reason to
type SQLRepositoryOption func(*SQLRepositoryImpl)

// WithLocalProbe tells the repository how the local probe is known to be online. Without it the
// local probe is Peekaping's own process, which is always online (shared.LocalProbeIsTheProcess).
func WithLocalProbe(liveness shared.LocalProbeLiveness) SQLRepositoryOption {
	return func(r *SQLRepositoryImpl) { r.localProbe = liveness }
}

func NewSQLRepository(db *bun.DB, opts ...SQLRepositoryOption) MonitorRepository {
	r := &SQLRepositoryImpl{db: db}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// sqlRepositoryParams are what the SQL repository is built from in an install
type sqlRepositoryParams struct {
	dig.In

	DB *bun.DB
	// LocalProbe is provided by internal/app: the process itself in single mode, the local probe's
	// instances in cluster mode. A container without it has the process.
	LocalProbe shared.LocalProbeLiveness `optional:"true"`
}

func provideSQLRepository(p sqlRepositoryParams) MonitorRepository {
	return NewSQLRepository(p.DB, WithLocalProbe(p.LocalProbe))
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, workspaceID workspace.ID, monitor *Model) (*Model, error) {
	sm := toSQLModel(monitor)
	sm.ID = uuid.New().String()
	sm.WorkspaceID = string(workspaceID) // whatever the monitor says
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, pushTokenConflict(err)
	}

	// A monitor without probes runs on the local one
	probeIDs := monitor.ProbeIDs
	if len(probeIDs) == 0 {
		probeIDs = []string{shared.LocalProbeID}
	}
	if err := r.replaceProbes(ctx, sm.ID, probeIDs); err != nil {
		return nil, err
	}

	// Read the probes back, so the create answers as every read does: in id order, with where
	// the monitor runs
	return r.toDomainModelWithProbes(ctx, sm)
}

// replaceProbes makes the probes the only ones the monitor runs on. A probe that stays keeps its
// row, and with it the state the row holds. The caller's transaction, if there is one, decides
// whether the monitor and its probes are stored together.
func (r *SQLRepositoryImpl) replaceProbes(ctx context.Context, monitorID string, probeIDs []string) error {
	db := dbtx.DB(ctx, r.db)
	remove := db.NewDelete().Model((*sqlProbeLink)(nil)).Where("monitor_id = ?", monitorID)
	if len(probeIDs) > 0 {
		remove = remove.Where("probe_id NOT IN (?)", bun.In(probeIDs))
	}
	if _, err := remove.Exec(ctx); err != nil {
		return err
	}
	if len(probeIDs) == 0 {
		return nil
	}
	links := make([]*sqlProbeLink, len(probeIDs))
	for i, probeID := range probeIDs {
		links[i] = &sqlProbeLink{MonitorID: monitorID, ProbeID: probeID}
	}
	_, err := db.NewInsert().Model(&links).On("CONFLICT (monitor_id, probe_id) DO NOTHING").Exec(ctx)
	return err
}

// assignChunk is how many monitors AddProbeToAll writes with one statement
const assignChunk = 500

// AddProbeToAllAnyWorkspace adds the probe to the monitors it can run, with a read and then writes in chunks.
// Adding a probe can't break a monitor's location rule: any and all have no number, and an at_least
// number stays within the larger count of probes. So the rule needs no check here, as it does when
// an update sets the probes.
func (r *SQLRepositoryImpl) AddProbeToAllAnyWorkspace(ctx context.Context, probeID string, supportedTypes []string) ([]string, error) {
	db := dbtx.DB(ctx, r.db)

	// This is the one place that decides which monitors a probe is assigned. A probe belongs to the
	// install, so it is assigned the monitors of every workspace (ADR 0005 section 10).
	var rows []struct {
		ID string `bun:"id"`
		// Unlinked is true for a monitor with no probe rows, which runs on the local probe
		Unlinked bool `bun:"unlinked"`
	}
	query := db.NewSelect().Model((*sqlModel)(nil)).
		ColumnExpr("m.id AS id").
		ColumnExpr("NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_id = m.id) AS unlinked").
		Where("m.type <> ?", pushType).
		Where("NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_id = m.id AND probe_id = ?)", probeID).
		Order("m.id")
	if len(supportedTypes) > 0 {
		query = query.Where("m.type IN (?)", bun.In(supportedTypes))
	}
	if err := query.Scan(ctx, &rows); err != nil {
		return nil, err
	}

	ids := make([]string, len(rows))
	var unlinked []string
	for i, row := range rows {
		ids[i] = row.ID
		if row.Unlinked {
			unlinked = append(unlinked, row.ID)
		}
	}

	// A monitor with no rows keeps running on the local probe: its row is written before the new
	// probe's, since a monitor with any row runs on those alone (see FindActiveByProbe)
	if err := r.insertProbeLinks(ctx, db, unlinked, shared.LocalProbeID); err != nil {
		return nil, err
	}
	if err := r.insertProbeLinks(ctx, db, ids, probeID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, chunk := range chunked(ids, assignChunk) {
		if _, err := db.NewUpdate().Model((*sqlModel)(nil)).
			Set("updated_at = ?", now).
			Where("id IN (?)", bun.In(chunk)).
			Exec(ctx); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// insertProbeLinks makes the probe one of the probes of each monitor, in chunks. A monitor that
// runs on it already keeps its row, and with it the state the row holds.
func (r *SQLRepositoryImpl) insertProbeLinks(ctx context.Context, db bun.IDB, monitorIDs []string, probeID string) error {
	for _, chunk := range chunked(monitorIDs, assignChunk) {
		links := make([]*sqlProbeLink, len(chunk))
		for i, monitorID := range chunk {
			links[i] = &sqlProbeLink{MonitorID: monitorID, ProbeID: probeID}
		}
		if _, err := db.NewInsert().Model(&links).On("CONFLICT (monitor_id, probe_id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// chunked splits ids into slices of at most size
func chunked(ids []string, size int) [][]string {
	var chunks [][]string
	for start := 0; start < len(ids); start += size {
		chunks = append(chunks, ids[start:min(start+size, len(ids))])
	}
	return chunks
}

// locationsOf reads the probe rows of the monitors, with one query for the lot, in id order
func (r *SQLRepositoryImpl) locationsOf(ctx context.Context, db bun.IDB, ids []string) ([]*sqlLocation, error) {
	var rows []*sqlLocation
	err := db.NewSelect().Model(&rows).
		ColumnExpr("mp.monitor_id, mp.probe_id, mp.status, mp.last_ping, mp.last_heartbeat_at").
		ColumnExpr("COALESCE(p.name, '') AS probe_name, COALESCE(p.status, '') AS probe_status").
		ColumnExpr("m.type AS monitor_type").
		Join("LEFT JOIN probes AS p ON p.id = mp.probe_id").
		Join("JOIN monitors AS m ON m.id = mp.monitor_id").
		Where("mp.monitor_id IN (?)", bun.In(ids)).
		Order("mp.monitor_id", "mp.probe_id").
		Scan(ctx)
	return rows, err
}

// RecordLocationAnyWorkspace stores what a result left on the monitor's row for the probe that ran it, then
// calls judge with the monitor's recorded status and its rows from just before and just after, and
// records what judge returns. The monitor is held from the first read to the last write, so two
// results recorded at once see one another in turn: each one's recorded status is what the other
// recorded. PostgreSQL holds the monitor's row with SELECT ... FOR UPDATE. SQLite has no row locks:
// its transaction begins with BEGIN IMMEDIATE (see sqlitedb.DSN) and so holds the one writer for the
// whole database. A probe the monitor doesn't run on has no row to write, and none is made.
func (r *SQLRepositoryImpl) RecordLocationAnyWorkspace(ctx context.Context, monitorID string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error {
	return dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		// The monitor's own row is what is held, so that a monitor without probe rows is held too
		if r.db.Dialect().Name() == dialect.PG {
			var locked []string
			lock := db.NewSelect().Model((*sqlModel)(nil)).Column("id").Where("id = ?", monitorID).For("UPDATE")
			if err := lock.Scan(ctx, &locked); err != nil {
				return err
			}
		}
		var recorded sql.NullInt64
		// A monitor that is gone has no row: nothing is recorded for it
		if err := db.NewSelect().Model((*sqlModel)(nil)).Column("locations_status").Where("id = ?", monitorID).Scan(ctx, &recorded); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		judgement := shared.LocationJudgement{}
		if recorded.Valid {
			status := shared.MonitorStatus(recorded.Int64)
			judgement.Recorded = &status
		}
		rows, err := r.locationsOf(ctx, db, []string{monitorID})
		if err != nil {
			return err
		}
		judgement.Before = r.toLocations(rows)

		update := db.NewUpdate().Model((*sqlProbeLink)(nil)).
			Where("monitor_id = ?", monitorID).Where("probe_id = ?", state.ProbeID).
			Set("status = ?", statusColumn(state.Status)).
			Set("last_ping = ?", state.LastPing)
		if state.LastHeartbeatAt != nil {
			update = update.Set("last_heartbeat_at = ?", state.LastHeartbeatAt.UTC())
		} else {
			update = update.Set("last_heartbeat_at = ?", nil)
		}
		if _, err := update.Exec(ctx); err != nil {
			return err
		}
		rows, err = r.locationsOf(ctx, db, []string{monitorID})
		if err != nil {
			return err
		}
		judgement.After = r.toLocations(rows)

		_, err = db.NewUpdate().Model((*sqlModel)(nil)).Set("locations_status = ?", statusColumn(judge(judgement))).Where("id = ?", monitorID).Exec(ctx)
		return err
	})
}

// ResetLocations clears the state of the monitor's rows and its recorded status. The monitor is
// looked up in the workspace first, so a monitor of another workspace keeps its state.
func (r *SQLRepositoryImpl) ResetLocations(ctx context.Context, workspaceID workspace.ID, monitorID string) error {
	return dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		result, err := db.NewUpdate().Model((*sqlModel)(nil)).Set("locations_status = NULL").
			Where("id = ?", monitorID).Where("workspace_id = ?", string(workspaceID)).Exec(ctx)
		if err := requireOneRow(result, err, monitorID); err != nil {
			return err
		}
		_, err = db.NewUpdate().Model((*sqlProbeLink)(nil)).
			Set("status = NULL").Set("last_ping = NULL").Set("last_heartbeat_at = NULL").
			Where("monitor_id = ?", monitorID).Exec(ctx)
		return err
	})
}

// requireOneRow turns a write that changed no row into ErrMonitorNotFound: the monitor doesn't
// exist, or belongs to another workspace, and the caller can't tell which
func requireOneRow(result sql.Result, err error, id string) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
	}
	return nil
}

func statusColumn(status *shared.MonitorStatus) *int {
	if status == nil {
		return nil
	}
	column := int(*status)
	return &column
}

func (r *SQLRepositoryImpl) toLocations(rows []*sqlLocation) []shared.MonitorLocation {
	locations := make([]shared.MonitorLocation, len(rows))
	for i, row := range rows {
		locations[i] = row.toLocation(r.localProbe)
	}
	return locations
}

// toDomainModels converts the monitors read and adds their probes, with one query for the lot.
// A monitor without a row runs on the local probe: one created by a process from before probes
// during an upgrade, or copied from MongoDB.
func (r *SQLRepositoryImpl) toDomainModels(ctx context.Context, sms []*sqlModel) ([]*Model, error) {
	var models []*Model
	if len(sms) == 0 {
		return models, nil
	}
	ids := make([]string, len(sms))
	byID := make(map[string]*Model, len(sms))
	for i, sm := range sms {
		model := toDomainModelFromSQL(sm)
		models = append(models, model)
		ids[i], byID[sm.ID] = sm.ID, model
	}

	rows, err := r.locationsOf(ctx, dbtx.DB(ctx, r.db), ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if model := byID[row.MonitorID]; model != nil {
			model.ProbeIDs = append(model.ProbeIDs, row.ProbeID)
			model.Locations = append(model.Locations, row.toLocation(r.localProbe))
		}
	}
	for _, model := range models {
		if len(model.ProbeIDs) == 0 {
			model.ProbeIDs = []string{shared.LocalProbeID}
			model.Locations = []shared.MonitorLocation{localLocation()}
		}
	}
	return models, nil
}

// toDomainModelWithProbes is toDomainModels for the one monitor
func (r *SQLRepositoryImpl) toDomainModelWithProbes(ctx context.Context, sm *sqlModel) (*Model, error) {
	models, err := r.toDomainModels(ctx, []*sqlModel{sm})
	if err != nil {
		return nil, err
	}
	return models[0], nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return r.find(ctx, &workspaceID, id)
}

func (r *SQLRepositoryImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return r.find(ctx, nil, id)
}

// find reads the monitor, in the workspace when there is one. A monitor of another workspace is
// not found.
func (r *SQLRepositoryImpl) find(ctx context.Context, workspaceID *workspace.ID, id string) (*Model, error) {
	sm := new(sqlModel)
	query := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("m.id = ?", id)
	if workspaceID != nil {
		query = query.Where("m.workspace_id = ?", string(*workspaceID))
	}
	if err := query.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.toDomainModelWithProbes(ctx, sm)
}

func (r *SQLRepositoryImpl) FindByIDs(ctx context.Context, workspaceID workspace.ID, ids []string) ([]*Model, error) {
	return r.findMany(ctx, &workspaceID, ids)
}

func (r *SQLRepositoryImpl) FindByIDsAnyWorkspace(ctx context.Context, ids []string) ([]*Model, error) {
	return r.findMany(ctx, nil, ids)
}

func (r *SQLRepositoryImpl) findMany(ctx context.Context, workspaceID *workspace.ID, ids []string) ([]*Model, error) {
	if len(ids) == 0 {
		return []*Model{}, nil
	}

	var sms []*sqlModel
	query := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("m.id IN (?)", bun.In(ids))
	if workspaceID != nil {
		query = query.Where("m.workspace_id = ?", string(*workspaceID))
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindAll(
	ctx context.Context,
	workspaceID workspace.ID,
	page int,
	limit int,
	q string,
	active *bool,
	status *int,
	tagIds []string,
) ([]*Model, error) {
	return r.findAll(ctx, &workspaceID, page, limit, q, active, status, tagIds)
}

func (r *SQLRepositoryImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return r.findAll(ctx, nil, page, limit, "", nil, nil, nil)
}

func (r *SQLRepositoryImpl) findAll(
	ctx context.Context,
	workspaceID *workspace.ID,
	page int,
	limit int,
	q string,
	active *bool,
	status *int,
	tagIds []string,
) ([]*Model, error) {
	query := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil))

	if workspaceID != nil {
		query = query.Where("m.workspace_id = ?", string(*workspaceID))
	}

	// If tagIds filtering is requested, use JOIN
	if len(tagIds) > 0 {
		// Join with monitor_tags table and filter by tag IDs
		query = query.
			Join("INNER JOIN monitor_tags mt ON m.id = mt.monitor_id").
			Where("mt.tag_id IN (?)", bun.In(tagIds)).
			Group("m.id") // Group by monitor ID to avoid duplicates when monitor has multiple matching tags
	}

	if q != "" {
		// Use LIKE instead of ILIKE for better database compatibility
		query = query.Where("LOWER(name) LIKE ?", "%"+q+"%")
	}

	if active != nil {
		query = query.Where("active = ?", *active)
	}

	if status != nil {
		query = query.Where("status = ?", *status)
	}

	// id breaks ties, so offset paging neither skips nor repeats monitors created at the same time
	query = query.Order("m.created_at DESC", "m.id DESC").
		Limit(limit).
		Offset(page * limit)

	var sms []*sqlModel
	err := query.Scan(ctx, &sms)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindActiveAnyWorkspace(ctx context.Context) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("active = ?", true).
		Order("id DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindActivePaginatedAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("active = ?", true).
		Order("id DESC").
		Limit(limit).
		Offset(page * limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindActiveByProbeAnyWorkspace(ctx context.Context, probeID string, page int, limit int) ([]*Model, error) {
	var sms []*sqlModel
	query := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("m.active = ?", true)
	if probeID == shared.LocalProbeID {
		// A monitor without a row runs on the local probe (see toDomainModels)
		query = query.Where("(m.id IN (SELECT monitor_id FROM monitor_probes WHERE probe_id = ?) OR NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_id = m.id))", probeID)
	} else {
		query = query.Where("m.id IN (SELECT monitor_id FROM monitor_probes WHERE probe_id = ?)", probeID)
	}
	err := query.
		Order("m.id DESC").
		Limit(limit).
		Offset(page * limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, monitor *Model) error {
	sm := toSQLModel(monitor)
	sm.UpdatedAt = time.Now()

	// The workspace is in the where clause and never in the set: a monitor can't be moved
	result, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model(sm).
		Where("id = ?", id).
		Where("workspace_id = ?", string(workspaceID)).
		ExcludeColumn("id", "workspace_id", "created_at", "status").
		Exec(ctx)

	if err != nil {
		return pushTokenConflict(err)
	}

	rowAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowAffected == 0 {
		return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
	}

	// A monitor without probes in the update keeps the ones it has
	if monitor.ProbeIDs != nil {
		return r.replaceProbes(ctx, id, monitor.ProbeIDs)
	}
	return nil
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, monitor *UpdateModel) error {
	return r.updatePartial(ctx, &workspaceID, id, monitor)
}

func (r *SQLRepositoryImpl) UpdatePartialAnyWorkspace(ctx context.Context, id string, monitor *UpdateModel) error {
	return r.updatePartial(ctx, nil, id, monitor)
}

// updatePartial changes the fields the update sets. In a workspace, a monitor of another workspace
// matches no row and is ErrMonitorNotFound. Without one, a monitor that is gone is not an error.
func (r *SQLRepositoryImpl) updatePartial(ctx context.Context, workspaceID *workspace.ID, id string, monitor *UpdateModel) error {
	query := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)
	if workspaceID != nil {
		query = query.Where("workspace_id = ?", string(*workspaceID))
	}

	hasUpdates := false

	if monitor.Type != nil {
		query = query.Set("type = ?", *monitor.Type)
		hasUpdates = true
	}
	if monitor.Name != nil {
		query = query.Set("name = ?", *monitor.Name)
		hasUpdates = true
	}
	if monitor.Interval != nil {
		query = query.Set("interval = ?", *monitor.Interval)
		hasUpdates = true
	}
	if monitor.Timeout != nil {
		query = query.Set("timeout = ?", *monitor.Timeout)
		hasUpdates = true
	}
	if monitor.MaxRetries != nil {
		query = query.Set("max_retries = ?", *monitor.MaxRetries)
		hasUpdates = true
	}
	if monitor.RetryInterval != nil {
		query = query.Set("retry_interval = ?", *monitor.RetryInterval)
		hasUpdates = true
	}
	if monitor.ResendInterval != nil {
		query = query.Set("resend_interval = ?", *monitor.ResendInterval)
		hasUpdates = true
	}
	if monitor.LocationRule != nil {
		// The threshold goes with the rule: a rule without one clears it
		query = query.Set("location_rule = ?", string(*monitor.LocationRule)).Set("location_threshold = ?", monitor.LocationThreshold)
		hasUpdates = true
	}
	if monitor.Active != nil {
		query = query.Set("active = ?", *monitor.Active)
		hasUpdates = true
	}
	if monitor.Status != nil {
		query = query.Set("status = ?", *monitor.Status)
		hasUpdates = true
	}
	if monitor.Config != nil {
		query = query.Set("config = ?", *monitor.Config)
		hasUpdates = true
	}
	if monitor.ProxyId != nil {
		if *monitor.ProxyId == "" {
			// Set to NULL when ProxyId is empty string
			query = query.Set("proxy_id = ?", nil)
		} else {
			query = query.Set("proxy_id = ?", *monitor.ProxyId)
		}
		hasUpdates = true
	}
	if monitor.PushToken != nil {
		if *monitor.PushToken == "" {
			// NULL, not an empty string, for a monitor without a token
			query = query.Set("push_token = ?", nil)
		} else {
			query = query.Set("push_token = ?", *monitor.PushToken)
		}
		hasUpdates = true
	}

	if monitor.ProbeIDs != nil {
		hasUpdates = true
	}

	if !hasUpdates {
		if workspaceID == nil {
			return nil
		}
		// Nothing to write, but a monitor of another workspace is still not found
		found, err := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil)).
			Where("id = ?", id).Where("workspace_id = ?", string(*workspaceID)).Exists(ctx)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
		}
		return nil
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	// The monitor is written before its probes, so that the probes of a monitor of another
	// workspace are never touched
	result, err := query.Exec(ctx)
	if err != nil {
		return pushTokenConflict(err)
	}
	if workspaceID != nil {
		if err := requireOneRow(result, nil, id); err != nil {
			return err
		}
	}
	if monitor.ProbeIDs != nil {
		return r.replaceProbes(ctx, id, monitor.ProbeIDs)
	}
	return nil
}

// pushTokenConflict is the error for a write of a monitor that the database refused because
// another monitor holds its push token, and err otherwise. The push token is the only unique
// column such a write can break: the id of a new monitor is made here.
func pushTokenConflict(err error) error {
	if dbtx.IsUniqueViolation(err) {
		return pushTokenAlreadyUsedError()
	}
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	result, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlModel)(nil)).
		Where("id = ?", id).Where("workspace_id = ?", string(workspaceID)).Exec(ctx)
	return requireOneRow(result, err, id)
}

func (r *SQLRepositoryImpl) RemoveProxyReference(ctx context.Context, workspaceID workspace.ID, proxyId string) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model((*sqlModel)(nil)).
		Set("proxy_id = ?", nil).
		Where("proxy_id = ?", proxyId).
		Where("workspace_id = ?", string(workspaceID)).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindByProxyId(ctx context.Context, workspaceID workspace.ID, proxyId string) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("proxy_id = ?", proxyId).
		Where("workspace_id = ?", string(workspaceID)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindOneByPushTokenAnyWorkspace(ctx context.Context, pushToken string) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("push_token = ?", pushToken).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.toDomainModelWithProbes(ctx, sm)
}
