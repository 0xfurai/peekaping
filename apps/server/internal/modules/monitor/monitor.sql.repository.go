package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/limits"
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
	// WorkspaceSuspended is read with the monitor by the lookups that ask for it (withSuspension)
	WorkspaceSuspended bool `bun:"workspace_suspended,scanonly"`
	// WorkspaceSuspendedByAdmin is whether an instance admin holds the workspace (PP-372), read with it
	WorkspaceSuspendedByAdmin bool `bun:"workspace_suspended_by_admin,scanonly"`
}

// workspaceSuspended is true for a monitor whose workspace is suspended (PP-155), by the email
// verification or by an instance admin (PP-372): either hold keeps the monitor from running. It is a
// primary-key lookup on workspaces for each monitor row.
const workspaceSuspended = "EXISTS (SELECT 1 FROM workspaces AS ws WHERE ws.id = m.workspace_id AND (ws.suspended_at IS NOT NULL OR ws.admin_suspended_at IS NOT NULL))"

// workspaceSuspendedByAdmin is true for a monitor whose workspace an instance admin holds, which a
// push says in its refusal
const workspaceSuspendedByAdmin = "EXISTS (SELECT 1 FROM workspaces AS ws WHERE ws.id = m.workspace_id AND ws.admin_suspended_at IS NOT NULL)"

// withSuspension reads the monitor's own columns and whether its workspace is suspended, and
// whether an instance admin holds it
func withSuspension(q *bun.SelectQuery) *bun.SelectQuery {
	return q.ColumnExpr("m.*").ColumnExpr(workspaceSuspended + " AS workspace_suspended").ColumnExpr(workspaceSuspendedByAdmin + " AS workspace_suspended_by_admin")
}

// inRunningWorkspace keeps the monitors whose workspace is not suspended: the filter of every query
// that picks monitors to run or to check for missed beats
func inRunningWorkspace(q *bun.SelectQuery) *bun.SelectQuery {
	return q.Where("NOT " + workspaceSuspended)
}

// runnable keeps the monitors that may run or be checked for missed beats: those in a workspace
// that is not suspended, when the install turns monitor types off of a type that is on, and when a
// workspace has more active monitors than its limit, those within the limit (PP-160)
func (r *SQLRepositoryImpl) runnable(ctx context.Context, q *bun.SelectQuery) (*bun.SelectQuery, error) {
	q = inRunningWorkspace(q)
	if len(r.disabledTypes) > 0 {
		q = q.Where("m.type NOT IN (?)", bun.In(r.disabledTypes))
	}
	over, err := r.overLimit(ctx, nil)
	if err != nil || len(over) == 0 {
		return q, err
	}
	// Only the monitors of the workspaces over their limit have their place counted
	ids := make([]string, 0, len(over))
	args := []any{nil, true}
	cases := ""
	for id, limit := range over {
		ids = append(ids, string(id))
		cases += " WHEN ? THEN ?"
		args = append(args, string(id), limit)
	}
	args[0] = bun.In(ids)
	return q.Where("(m.workspace_id NOT IN (?) OR "+activeAhead+" < CASE m.workspace_id"+cases+" END)", args...), nil
}

// activeAhead counts the active monitors of the row's workspace that come before the row, by
// created_at and then id: the monitor is within a limit of n active monitors while the count is
// below n. It takes true as its one argument.
const activeAhead = "(SELECT COUNT(*) FROM monitors AS ahead WHERE ahead.workspace_id = m.workspace_id AND ahead.active = ?" +
	" AND (ahead.created_at < m.created_at OR (ahead.created_at = m.created_at AND ahead.id < m.id)))"

// overLimit returns the workspaces that have more active monitors than their limit, with the
// limit: of workspaceIDs, or of the whole install when it is nil. Without a limit of active
// monitors anywhere it reads nothing.
func (r *SQLRepositoryImpl) overLimit(ctx context.Context, workspaceIDs []string) (map[workspace.ID]int, error) {
	snapshot, err := r.limitSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	lowest := snapshot.Lowest(limits.Monitors)
	if lowest == nil {
		return nil, nil
	}
	// A transaction reads its own writes, so it counts them itself and keeps nothing
	if dbtx.DB(ctx, r.db) != bun.IDB(r.db) {
		return r.countOverLimit(ctx, snapshot, *lowest, workspaceIDs)
	}
	cached, ok, generation := r.overLimitCache.get(snapshot)
	if ok {
		return of(cached, workspaceIDs), nil
	}
	over, err := r.countOverLimit(ctx, snapshot, *lowest, workspaceIDs)
	if err != nil || workspaceIDs != nil {
		return over, err
	}
	r.overLimitCache.put(snapshot, over, generation)
	return over, nil
}

// limitSnapshot is every workspace's limits: an empty one without a source
func (r *SQLRepositoryImpl) limitSnapshot(ctx context.Context) (limits.Snapshot, error) {
	if r.limits == nil {
		return limits.Snapshot{}, nil
	}
	snapshot, err := r.limits.Snapshot(ctx)
	if err != nil {
		return limits.Snapshot{}, fmt.Errorf("read the workspaces' limits: %w", err)
	}
	return snapshot, nil
}

// activeCount is how many active monitors a workspace has
type activeCount struct {
	WorkspaceID string `bun:"workspace_id"`
	Active      int    `bun:"active_count"`
}

// countOverLimit counts the active monitors of the workspaces that have more than the lowest limit
// of any workspace, of workspaceIDs or of every workspace when it is nil, and returns those over
// their own limit
func (r *SQLRepositoryImpl) countOverLimit(ctx context.Context, snapshot limits.Snapshot, lowest int, workspaceIDs []string) (map[workspace.ID]int, error) {
	var counts []activeCount
	query := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil)).
		ColumnExpr("m.workspace_id AS workspace_id").ColumnExpr("COUNT(*) AS active_count").
		Where("m.active = ?", true).
		Group("m.workspace_id").
		Having("COUNT(*) > ?", lowest)
	if workspaceIDs != nil {
		if len(workspaceIDs) == 0 {
			return nil, nil
		}
		query = query.Where("m.workspace_id IN (?)", bun.In(workspaceIDs))
	}
	if err := query.Scan(ctx, &counts); err != nil {
		return nil, err
	}
	return workspacesOverLimit(snapshot, counts), nil
}

// workspacesOverLimit picks the workspaces with more active monitors than their own limit
func workspacesOverLimit(snapshot limits.Snapshot, counts []activeCount) map[workspace.ID]int {
	over := map[workspace.ID]int{}
	for _, count := range counts {
		id := workspace.ID(count.WorkspaceID)
		if l, _ := snapshot.For(id); l.Monitors != nil && count.Active > *l.Monitors {
			over[id] = *l.Monitors
		}
	}
	return over
}

// of returns the workspaces of over that are among workspaceIDs, or over when it is nil
func of(over map[workspace.ID]int, workspaceIDs []string) map[workspace.ID]int {
	if workspaceIDs == nil {
		return over
	}
	some := map[workspace.ID]int{}
	for _, id := range workspaceIDs {
		if limit, ok := over[workspace.ID(id)]; ok {
			some[workspace.ID(id)] = limit
		}
	}
	return some
}

// overLimitCache keeps the workspaces over their limit of active monitors, of the whole install,
// for a few seconds: a pass over many pages of monitors to run counts them once, not once a page.
// It is kept with the limits it was counted by, and a change of them counts again. This process
// forgets it when it creates, resumes, pauses or deletes a monitor; another replica's change takes
// effect within it, as a change of limits does (limits.DefaultCacheFor).
type overLimitCache struct {
	mu       sync.Mutex
	over     map[workspace.ID]int
	snapshot limits.Snapshot
	at       time.Time
	kept     bool
	// generation counts the changes this process made, so that a count that began before one is
	// not kept after it
	generation uint64
}

// get returns what is kept, when it was counted by snapshot and is fresh, and otherwise the
// generation to put a new count with
func (c *overLimitCache) get(snapshot limits.Snapshot) (map[workspace.ID]int, bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kept && time.Since(c.at) < limits.DefaultCacheFor && reflect.DeepEqual(c.snapshot, snapshot) {
		return c.over, true, c.generation
	}
	return nil, false, c.generation
}

func (c *overLimitCache) put(snapshot limits.Snapshot, over map[workspace.ID]int, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation == generation {
		c.over, c.snapshot, c.at, c.kept = over, snapshot, time.Now(), true
	}
}

// forget drops what is kept, after a write that may change which monitors are past a limit
func (c *overLimitCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.over, c.kept = nil, false
	c.generation++
}

// markOverLimit says of each active monitor whether it is past its workspace's limit of active
// monitors: one query for the lot, and one more when a workspace among them is over its limit
func (r *SQLRepositoryImpl) markOverLimit(ctx context.Context, models []*Model) error {
	if r.limits == nil {
		return nil
	}
	workspaceIDs := activeWorkspaceIDs(models)
	if len(workspaceIDs) == 0 {
		return nil
	}
	over, err := r.overLimit(ctx, workspaceIDs)
	if err != nil || len(over) == 0 {
		return err
	}
	ahead, err := r.activePlaces(ctx, activeIDsIn(models, over))
	if err != nil {
		return err
	}
	for _, m := range models {
		if limit, ok := over[m.WorkspaceID]; ok && m.Active {
			m.OverLimit = ahead[m.ID] >= limit
		}
	}
	return nil
}

// activeWorkspaceIDs are the workspaces of the active monitors, once each, in the order they come
func activeWorkspaceIDs(models []*Model) []string {
	var workspaceIDs []string
	seen := map[workspace.ID]bool{}
	for _, m := range models {
		if m.Active && !seen[m.WorkspaceID] {
			seen[m.WorkspaceID] = true
			workspaceIDs = append(workspaceIDs, string(m.WorkspaceID))
		}
	}
	return workspaceIDs
}

// activeIDsIn are the IDs of the active monitors in the workspaces that are over their limit
func activeIDsIn(models []*Model, over map[workspace.ID]int) []string {
	var ids []string
	for _, m := range models {
		if _, ok := over[m.WorkspaceID]; ok && m.Active {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// activePlaces counts, for each monitor, the active monitors of its workspace that come before it
func (r *SQLRepositoryImpl) activePlaces(ctx context.Context, ids []string) (map[string]int, error) {
	var places []struct {
		ID    string `bun:"id"`
		Ahead int    `bun:"ahead"`
	}
	if err := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil)).
		ColumnExpr("m.id AS id").ColumnExpr(activeAhead+" AS ahead", true).
		Where("m.id IN (?)", bun.In(ids)).
		Scan(ctx, &places); err != nil {
		return nil, err
	}
	ahead := make(map[string]int, len(places))
	for _, place := range places {
		ahead[place.ID] = place.Ahead
	}
	return ahead, nil
}

// typeDisabled is whether the install turns the monitor type off
func (r *SQLRepositoryImpl) typeDisabled(monitorType string) bool {
	return slices.Contains(r.disabledTypes, monitorType)
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

		WorkspaceSuspended:        sm.WorkspaceSuspended,
		WorkspaceSuspendedByAdmin: sm.WorkspaceSuspendedByAdmin,
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
	// disabledTypes are the monitor types the install turns off (DISABLED_MONITOR_TYPES, PP-157)
	disabledTypes []string
	// limits are the workspaces' limits (PP-160). Without them no workspace has any.
	limits limits.Source
	// overLimitCache keeps the workspaces over their limit across the pages of a read
	overLimitCache overLimitCache
}

// SQLRepositoryOption changes how the SQL repository behaves, for an install that has a reason to
type SQLRepositoryOption func(*SQLRepositoryImpl)

// WithLocalProbe tells the repository how the local probe is known to be online. Without it the
// local probe is Peekaping's own process, which is always online (shared.LocalProbeIsTheProcess).
func WithLocalProbe(liveness shared.LocalProbeLiveness) SQLRepositoryOption {
	return func(r *SQLRepositoryImpl) { r.localProbe = liveness }
}

// WithRepositoryDisabledTypes tells the repository which monitor types the install turns off. Its lists of
// monitors to run leave them out, and a monitor it reads says whether its type is one of them.
// Without it no type is disabled.
func WithRepositoryDisabledTypes(types []string) SQLRepositoryOption {
	return func(r *SQLRepositoryImpl) { r.disabledTypes = types }
}

// WithRepositoryLimits tells the repository each workspace's limits. Its lists of monitors to run
// leave out the active monitors past a workspace's limit of active monitors, the newest first, and a
// monitor it reads says whether it is one of them (OverLimit). Without it no workspace has limits.
func WithRepositoryLimits(source limits.Source) SQLRepositoryOption {
	return func(r *SQLRepositoryImpl) { r.limits = source }
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
	// Config is the install's settings. A container without it disables no monitor type.
	Config *config.Config `optional:"true"`
	// Limits are the workspaces' limits, from package limits. A container without them has none.
	Limits limits.Source `optional:"true"`
}

func provideSQLRepository(p sqlRepositoryParams) MonitorRepository {
	opts := []SQLRepositoryOption{WithLocalProbe(p.LocalProbe), WithRepositoryLimits(p.Limits)}
	if p.Config != nil {
		opts = append(opts, WithRepositoryDisabledTypes(p.Config.DisabledMonitorTypeList()))
	}
	return NewSQLRepository(p.DB, opts...)
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
	if sm.Active {
		r.overLimitCache.forget()
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

// errWorkspaceBelowHeld ends an assignment of a probe that found a workspace to hold below one it
// holds already: a monitor was created there in between. Held then, the workspaces would be held
// out of id order, and two assignments at once could each wait for one the other holds. The
// assignment is undone instead, its transaction rolled back, and made again from the start
// (AssignProbeToAllAnyWorkspace does).
var errWorkspaceBelowHeld = errors.New("found a workspace to hold below one held already")

// AddProbeToAllAnyWorkspace adds the probe to the monitors it can run, with a read and then writes in chunks.
// A monitor that runs on as many probes as its workspace's limit allows is skipped, and counted.
// Adding a probe can't break a monitor's location rule: any and all have no number, and an at_least
// number stays within the larger count of probes. So the rule needs no check here, as it does when
// an update sets the probes.
func (r *SQLRepositoryImpl) AddProbeToAllAnyWorkspace(ctx context.Context, probeID string, supportedTypes []string) ([]string, int, error) {
	db := dbtx.DB(ctx, r.db)
	candidates := func() *bun.SelectQuery { return assignCandidates(db, probeID, supportedTypes) }
	snapshot, err := r.limitSnapshot(ctx)
	if err != nil {
		return nil, 0, err
	}

	// The workspaces with a limit on probes are held before their monitors' probes are counted, and
	// until the new links are committed, so that an update of a monitor's probes, or another
	// assignment, can't add one beside this one past the limit (limits.LockWorkspace). They are
	// held in id order, so that two assignments at once never each wait for one the other holds.
	// One found later that sorts above every one held keeps that order and is held then; one that
	// sorts below ends the assignment with errWorkspaceBelowHeld.
	holds := &workspaceHolds{held: map[string]bool{}}
	if r.limits != nil {
		var workspaceIDs []string
		if err := candidates().ColumnExpr("m.workspace_id").Distinct().Scan(ctx, &workspaceIDs); err != nil {
			return nil, 0, err
		}
		if err := r.hold(ctx, holds, snapshot, workspaceIDs); err != nil {
			return nil, 0, err
		}
	}
	rows, err := r.readAssignable(ctx, holds, snapshot, candidates)
	if err != nil {
		return nil, 0, err
	}

	ids, unlinked, skipped := pickAssignable(snapshot, rows)
	if err := r.assignProbe(ctx, db, probeID, ids, unlinked); err != nil {
		return nil, 0, err
	}
	return ids, skipped, nil
}

// assignCandidates picks the monitors a probe is assigned. This is the one place that decides which
// monitors a probe is assigned. A probe belongs to the install, so it is assigned the monitors of
// every workspace (ADR 0005 section 10).
func assignCandidates(db bun.IDB, probeID string, supportedTypes []string) *bun.SelectQuery {
	query := db.NewSelect().Model((*sqlModel)(nil)).
		Where("m.type <> ?", pushType).
		Where("NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_id = m.id AND probe_id = ?)", probeID)
	if len(supportedTypes) > 0 {
		query = query.Where("m.type IN (?)", bun.In(supportedTypes))
	}
	return query
}

// workspaceHolds are the workspaces an assignment of a probe holds, and the highest of them
type workspaceHolds struct {
	held    map[string]bool
	highest string
}

// hold holds the workspaces among workspaceIDs that have a limit on probes and aren't held yet, in
// id order. It is errWorkspaceBelowHeld when one sorts below a workspace held already.
func (r *SQLRepositoryImpl) hold(ctx context.Context, holds *workspaceHolds, snapshot limits.Snapshot, workspaceIDs []string) error {
	var limited []string
	for _, id := range workspaceIDs {
		if l, _ := snapshot.For(workspace.ID(id)); l.ProbesPerMonitor != nil && !holds.held[id] {
			limited = append(limited, id)
		}
	}
	slices.Sort(limited)
	limited = slices.Compact(limited)
	if len(limited) > 0 && len(holds.held) > 0 && limited[0] < holds.highest {
		return errWorkspaceBelowHeld
	}
	for _, id := range limited {
		if err := r.LockWorkspace(ctx, workspace.ID(id)); err != nil {
			return fmt.Errorf("lock the workspace: %w", err)
		}
		holds.held[id] = true
		holds.highest = id
	}
	return nil
}

// assignRow is a monitor a probe can be assigned
type assignRow struct {
	ID          string `bun:"id"`
	WorkspaceID string `bun:"workspace_id"`
	// Probes is how many probe rows the monitor has. One with none runs on the local probe.
	Probes int `bun:"probes"`
}

// readAssignable reads the monitors the probe can be assigned, in id order, holding the workspaces
// they are in
func (r *SQLRepositoryImpl) readAssignable(ctx context.Context, holds *workspaceHolds, snapshot limits.Snapshot, candidates func() *bun.SelectQuery) ([]assignRow, error) {
	var rows []assignRow
	for {
		rows = rows[:0]
		if err := candidates().
			ColumnExpr("m.id AS id").ColumnExpr("m.workspace_id AS workspace_id").
			ColumnExpr("(SELECT COUNT(*) FROM monitor_probes WHERE monitor_id = m.id) AS probes").
			Order("m.id").Scan(ctx, &rows); err != nil {
			return nil, err
		}
		// A monitor created since the read of the workspaces can be of one this doesn't hold yet. It
		// is held now, and the probes counted again, unless it sorts below one held: then the
		// assignment ends with errWorkspaceBelowHeld, to be made again.
		before := len(holds.held)
		workspaceIDs := make([]string, len(rows))
		for i, row := range rows {
			workspaceIDs[i] = row.WorkspaceID
		}
		if err := r.hold(ctx, holds, snapshot, workspaceIDs); err != nil {
			return nil, err
		}
		if len(holds.held) == before {
			return rows, nil
		}
	}
}

// pickAssignable picks the monitors that get the probe, those among them that have no probe rows yet,
// and counts the ones that run on as many probes as their workspace allows, which get none
func pickAssignable(snapshot limits.Snapshot, rows []assignRow) (ids, unlinked []string, skipped int) {
	ids = make([]string, 0, len(rows))
	for _, row := range rows {
		// A monitor that runs on as many probes as its workspace allows gets no more (PP-160)
		if l, _ := snapshot.For(workspace.ID(row.WorkspaceID)); l.ProbesPerMonitor != nil && max(row.Probes, 1) >= *l.ProbesPerMonitor {
			skipped++
			continue
		}
		ids = append(ids, row.ID)
		if row.Probes == 0 {
			unlinked = append(unlinked, row.ID)
		}
	}
	return ids, unlinked, skipped
}

// assignProbe links the probe to the monitors, and touches their updated_at
func (r *SQLRepositoryImpl) assignProbe(ctx context.Context, db bun.IDB, probeID string, ids, unlinked []string) error {
	// A monitor with no rows keeps running on the local probe: its row is written before the new
	// probe's, since a monitor with any row runs on those alone (see FindActiveByProbe)
	if err := r.insertProbeLinks(ctx, db, unlinked, shared.LocalProbeID); err != nil {
		return err
	}
	if err := r.insertProbeLinks(ctx, db, ids, probeID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, chunk := range chunked(ids, assignChunk) {
		if _, err := db.NewUpdate().Model((*sqlModel)(nil)).
			Set("updated_at = ?", now).
			Where("id IN (?)", bun.In(chunk)).
			Exec(ctx); err != nil {
			return err
		}
	}
	return nil
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
// whole database. A probe the monitor doesn't run on has no row to write, and none is made. The
// monitor of a suspended workspace has nothing recorded and nothing judged: it returns
// shared.ErrWorkspaceSuspended.
func (r *SQLRepositoryImpl) RecordLocationAnyWorkspace(ctx context.Context, monitorID string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error {
	return dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := r.holdMonitorRow(ctx, db, monitorID); err != nil {
			return err
		}
		recorded, err := r.readRecordable(ctx, db, monitorID)
		if err != nil {
			return err
		}
		judgement := shared.LocationJudgement{Recorded: recorded}
		if judgement.Before, err = r.locationsAt(ctx, db, monitorID); err != nil {
			return err
		}
		if err := writeLocationState(ctx, db, monitorID, state); err != nil {
			return err
		}
		if judgement.After, err = r.locationsAt(ctx, db, monitorID); err != nil {
			return err
		}

		_, err = db.NewUpdate().Model((*sqlModel)(nil)).Set("locations_status = ?", statusColumn(judge(judgement))).Where("id = ?", monitorID).Exec(ctx)
		return err
	})
}

// holdMonitorRow holds the monitor's own row on PostgreSQL, so that a monitor without probe rows is
// held too
func (r *SQLRepositoryImpl) holdMonitorRow(ctx context.Context, db bun.IDB, monitorID string) error {
	if r.db.Dialect().Name() != dialect.PG {
		return nil
	}
	var locked []string
	return db.NewSelect().Model((*sqlModel)(nil)).Column("id").Where("id = ?", monitorID).For("UPDATE").Scan(ctx, &locked)
}

// readRecordable reads the status recorded for the monitor, nil when there is none. It is
// shared.ErrWorkspaceSuspended for a monitor of a suspended workspace, and
// shared.ErrMonitorTypeDisabled for one whose type is off: nothing is recorded or judged for them.
func (r *SQLRepositoryImpl) readRecordable(ctx context.Context, db bun.IDB, monitorID string) (*shared.MonitorStatus, error) {
	var recorded sql.NullInt64
	var monitorType string
	var suspended bool
	// A monitor that is gone has no row: nothing is recorded for it
	if err := db.NewSelect().Model((*sqlModel)(nil)).Column("locations_status", "type").ColumnExpr(workspaceSuspended+" AS workspace_suspended").
		Where("m.id = ?", monitorID).Scan(ctx, &recorded, &monitorType, &suspended); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// A result that was in flight when the workspace was suspended is dropped (PP-155)
	if suspended {
		return nil, shared.ErrWorkspaceSuspended
	}
	// So is one for a monitor whose type was turned off since the check was dealt (PP-157)
	if r.typeDisabled(monitorType) {
		return nil, shared.ErrMonitorTypeDisabled
	}
	if !recorded.Valid {
		return nil, nil
	}
	status := shared.MonitorStatus(recorded.Int64)
	return &status, nil
}

// locationsAt reads the monitor's locations as they are now
func (r *SQLRepositoryImpl) locationsAt(ctx context.Context, db bun.IDB, monitorID string) ([]shared.MonitorLocation, error) {
	rows, err := r.locationsOf(ctx, db, []string{monitorID})
	if err != nil {
		return nil, err
	}
	return r.toLocations(rows), nil
}

// writeLocationState stores what a result left on the monitor's row for the probe that ran it
func writeLocationState(ctx context.Context, db bun.IDB, monitorID string, state shared.LocationState) error {
	update := db.NewUpdate().Model((*sqlProbeLink)(nil)).
		Where("monitor_id = ?", monitorID).Where("probe_id = ?", state.ProbeID).
		Set("status = ?", statusColumn(state.Status)).
		Set("last_ping = ?", state.LastPing)
	if state.LastHeartbeatAt != nil {
		update = update.Set("last_heartbeat_at = ?", state.LastHeartbeatAt.UTC())
	} else {
		update = update.Set("last_heartbeat_at = ?", nil)
	}
	_, err := update.Exec(ctx)
	return err
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
		model.TypeDisabled = r.typeDisabled(sm.Type)
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
	if err := r.markOverLimit(ctx, models); err != nil {
		return nil, err
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

func (r *SQLRepositoryImpl) FindWorkspaceIDAnyWorkspace(ctx context.Context, id string) (workspace.ID, bool, error) {
	var workspaceID string
	err := dbtx.DB(ctx, r.db).NewSelect().Table("monitors").Column("workspace_id").Where("id = ?", id).Scan(ctx, &workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return workspace.ID(workspaceID), true, nil
}

// find reads the monitor, in the workspace when there is one. A monitor of another workspace is
// not found.
func (r *SQLRepositoryImpl) find(ctx context.Context, workspaceID *workspace.ID, id string) (*Model, error) {
	sm := new(sqlModel)
	query := withSuspension(dbtx.DB(ctx, r.db).NewSelect().Model(sm)).Where("m.id = ?", id)
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
	query := withSuspension(dbtx.DB(ctx, r.db).NewSelect().Model(&sms)).
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
	query, err := r.runnable(ctx, dbtx.DB(ctx, r.db).NewSelect().Model(&sms))
	if err != nil {
		return nil, err
	}
	err = query.
		Where("m.active = ?", true).
		Order("m.id DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindActivePaginatedAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	var sms []*sqlModel
	query, err := r.runnable(ctx, dbtx.DB(ctx, r.db).NewSelect().Model(&sms))
	if err != nil {
		return nil, err
	}
	err = query.
		Where("m.active = ?", true).
		Order("m.id DESC").
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
	query, err := r.runnable(ctx, dbtx.DB(ctx, r.db).NewSelect().Model(&sms))
	if err != nil {
		return nil, err
	}
	query = query.Where("m.active = ?", true)
	if probeID == shared.LocalProbeID {
		// A monitor without a row runs on the local probe (see toDomainModels)
		query = query.Where("(m.id IN (SELECT monitor_id FROM monitor_probes WHERE probe_id = ?) OR NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_id = m.id))", probeID)
	} else {
		query = query.Where("m.id IN (SELECT monitor_id FROM monitor_probes WHERE probe_id = ?)", probeID)
	}
	err = query.
		Order("m.id DESC").
		Limit(limit).
		Offset(page * limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) LockWorkspace(ctx context.Context, workspaceID workspace.ID) error {
	return limits.LockWorkspace(ctx, r.db, workspaceID)
}

func (r *SQLRepositoryImpl) CountActive(ctx context.Context, workspaceID workspace.ID) (int, error) {
	return limits.CountActiveMonitors(ctx, r.db, workspaceID)
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
	r.overLimitCache.forget()

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

	sets := partialSets(monitor)
	for _, set := range sets {
		query = query.Set(set.column+" = ?", set.value)
	}

	if len(sets) == 0 && monitor.ProbeIDs == nil {
		return r.requireNothingToWriteFound(ctx, workspaceID, id)
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	// The monitor is written before its probes, so that the probes of a monitor of another
	// workspace are never touched
	result, err := query.Exec(ctx)
	if err != nil {
		return pushTokenConflict(err)
	}
	if monitor.Active != nil {
		r.overLimitCache.forget()
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

// requireNothingToWriteFound answers an update with nothing to write: a monitor of another workspace
// is still not found, and without a workspace there is nothing to look for
func (r *SQLRepositoryImpl) requireNothingToWriteFound(ctx context.Context, workspaceID *workspace.ID, id string) error {
	if workspaceID == nil {
		return nil
	}
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

// columnSet is a column an update sets, and the value it gets
type columnSet struct {
	column string
	value  any
}

// partialSets are the columns an update sets, in the order they are written, for the fields it has
func partialSets(monitor *UpdateModel) []columnSet {
	var sets []columnSet
	sets = setIfGiven(sets, "type", monitor.Type)
	sets = setIfGiven(sets, "name", monitor.Name)
	sets = setIfGiven(sets, "interval", monitor.Interval)
	sets = setIfGiven(sets, "timeout", monitor.Timeout)
	sets = setIfGiven(sets, "max_retries", monitor.MaxRetries)
	sets = setIfGiven(sets, "retry_interval", monitor.RetryInterval)
	sets = setIfGiven(sets, "resend_interval", monitor.ResendInterval)
	if monitor.LocationRule != nil {
		// The threshold goes with the rule: a rule without one clears it
		sets = append(sets, columnSet{"location_rule", string(*monitor.LocationRule)}, columnSet{"location_threshold", monitor.LocationThreshold})
	}
	sets = setIfGiven(sets, "active", monitor.Active)
	sets = setIfGiven(sets, "status", monitor.Status)
	sets = setIfGiven(sets, "config", monitor.Config)
	// NULL, not an empty string, for a monitor without a proxy or a token
	sets = setNullIfEmpty(sets, "proxy_id", monitor.ProxyId)
	sets = setNullIfEmpty(sets, "push_token", monitor.PushToken)
	return sets
}

// setIfGiven adds the column with the value, when the update has one
func setIfGiven[T any](sets []columnSet, column string, value *T) []columnSet {
	if value == nil {
		return sets
	}
	return append(sets, columnSet{column, *value})
}

// setNullIfEmpty adds the column with the value, or NULL when the value is empty, if the update has one
func setNullIfEmpty(sets []columnSet, column string, value *string) []columnSet {
	if value == nil {
		return sets
	}
	if *value == "" {
		return append(sets, columnSet{column, nil})
	}
	return append(sets, columnSet{column, *value})
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
	r.overLimitCache.forget()
	return requireOneRow(result, err, id)
}

func (r *SQLRepositoryImpl) RemoveProxyReference(ctx context.Context, workspaceID workspace.ID, proxyId string) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model((*sqlModel)(nil)).
		Set("proxy_id = ?", nil).
		Set("updated_at = ?", time.Now()).
		Where("proxy_id = ?", proxyId).
		Where("workspace_id = ?", string(workspaceID)).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) ProxyInWorkspace(ctx context.Context, workspaceID workspace.ID, proxyID string) (bool, error) {
	return dbtx.DB(ctx, r.db).NewSelect().Table("proxies").
		Where("id = ?", proxyID).Where("workspace_id = ?", string(workspaceID)).Exists(ctx)
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
	err := withSuspension(dbtx.DB(ctx, r.db).NewSelect().Model(sm)).Where("m.push_token = ?", pushToken).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.toDomainModelWithProbes(ctx, sm)
}
