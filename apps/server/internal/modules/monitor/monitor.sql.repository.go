package monitor

import (
	"context"
	"fmt"
	"slices"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:monitors,alias:m"`

	ID             string               `bun:"id,pk"`
	Type           string               `bun:"type,notnull"`
	Name           string               `bun:"name,notnull"`
	Interval       int                  `bun:"interval,notnull"`
	Timeout        int                  `bun:"timeout,notnull"`
	MaxRetries     int                  `bun:"max_retries,notnull"`
	RetryInterval  int                  `bun:"retry_interval,notnull"`
	ResendInterval int                  `bun:"resend_interval,notnull"`
	Active         bool                 `bun:"active,notnull"` // no Bun default: it would replace false with the column default
	Status         shared.MonitorStatus `bun:"status,notnull,default:0"`
	CreatedAt      time.Time            `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time            `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	Config         string               `bun:"config"`
	ProxyId        *string              `bun:"proxy_id"`
	PushToken      string               `bun:"push_token"`
}

// sqlProbeLink is the row that says a monitor runs on a probe
type sqlProbeLink struct {
	bun.BaseModel `bun:"table:monitor_probes,alias:mp"`

	MonitorID string `bun:"monitor_id,pk"`
	ProbeID   string `bun:"probe_id,pk"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	// Handle nil ProxyId by converting to empty string
	var proxyId string
	if sm.ProxyId != nil {
		proxyId = *sm.ProxyId
	}

	return &Model{
		ID:             sm.ID,
		Type:           sm.Type,
		Name:           sm.Name,
		Interval:       sm.Interval,
		Timeout:        sm.Timeout,
		MaxRetries:     sm.MaxRetries,
		RetryInterval:  sm.RetryInterval,
		ResendInterval: sm.ResendInterval,
		Active:         sm.Active,
		Status:         sm.Status,
		CreatedAt:      sm.CreatedAt,
		UpdatedAt:      sm.UpdatedAt,
		Config:         sm.Config,
		ProxyId:        proxyId,
		PushToken:      sm.PushToken,
	}
}

func toSQLModel(m *Model) *sqlModel {
	// Handle empty ProxyId by converting to NULL for PostgreSQL UUID compatibility
	var proxyId *string
	if m.ProxyId != "" {
		proxyId = &m.ProxyId
	}

	return &sqlModel{
		ID:             m.ID,
		Type:           m.Type,
		Name:           m.Name,
		Interval:       m.Interval,
		Timeout:        m.Timeout,
		MaxRetries:     m.MaxRetries,
		RetryInterval:  m.RetryInterval,
		ResendInterval: m.ResendInterval,
		Active:         m.Active,
		Status:         m.Status,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		Config:         m.Config,
		ProxyId:        proxyId,
		PushToken:      m.PushToken,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) MonitorRepository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, monitor *Model) (*Model, error) {
	sm := toSQLModel(monitor)
	sm.ID = uuid.New().String()
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	// A monitor without probes runs on the local one
	probeIDs := monitor.ProbeIDs
	if len(probeIDs) == 0 {
		probeIDs = []string{shared.LocalProbeID}
	}
	if err := r.replaceProbes(ctx, sm.ID, probeIDs); err != nil {
		return nil, err
	}

	created := toDomainModelFromSQL(sm)
	created.ProbeIDs = slices.Clone(probeIDs)
	return created, nil
}

// replaceProbes makes the probes the only ones the monitor runs on. The caller's transaction, if
// there is one, decides whether the monitor and its probes are stored together.
func (r *SQLRepositoryImpl) replaceProbes(ctx context.Context, monitorID string, probeIDs []string) error {
	db := dbtx.DB(ctx, r.db)
	if _, err := db.NewDelete().Model((*sqlProbeLink)(nil)).Where("monitor_id = ?", monitorID).Exec(ctx); err != nil {
		return err
	}
	links := make([]*sqlProbeLink, len(probeIDs))
	for i, probeID := range probeIDs {
		links[i] = &sqlProbeLink{MonitorID: monitorID, ProbeID: probeID}
	}
	_, err := db.NewInsert().Model(&links).Exec(ctx)
	return err
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

	var links []*sqlProbeLink
	err := dbtx.DB(ctx, r.db).NewSelect().Model(&links).
		Where("monitor_id IN (?)", bun.In(ids)).
		Order("monitor_id", "probe_id").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if model := byID[link.MonitorID]; model != nil {
			model.ProbeIDs = append(model.ProbeIDs, link.ProbeID)
		}
	}
	for _, model := range models {
		if len(model.ProbeIDs) == 0 {
			model.ProbeIDs = []string{shared.LocalProbeID}
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

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return r.toDomainModelWithProbes(ctx, sm)
}

func (r *SQLRepositoryImpl) FindByIDs(ctx context.Context, ids []string) ([]*Model, error) {
	if len(ids) == 0 {
		return []*Model{}, nil
	}

	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("id IN (?)", bun.In(ids)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	active *bool,
	status *int,
	tagIds []string,
) ([]*Model, error) {
	query := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil))

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

func (r *SQLRepositoryImpl) FindActive(ctx context.Context) ([]*Model, error) {
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

func (r *SQLRepositoryImpl) FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error) {
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

func (r *SQLRepositoryImpl) FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*Model, error) {
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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, monitor *Model) error {
	sm := toSQLModel(monitor)
	sm.UpdatedAt = time.Now()

	result, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model(sm).
		Where("id = ?", id).
		ExcludeColumn("id", "created_at", "status").
		Exec(ctx)

	if err != nil {
		return err
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

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, monitor *UpdateModel) error {
	query := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

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
		query = query.Set("push_token = ?", *monitor.PushToken)
		hasUpdates = true
	}

	if monitor.ProbeIDs != nil {
		if err := r.replaceProbes(ctx, id, monitor.ProbeIDs); err != nil {
			return err
		}
		hasUpdates = true
	}

	if !hasUpdates {
		return nil
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	_, err := query.Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) RemoveProxyReference(ctx context.Context, proxyId string) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model((*sqlModel)(nil)).
		Set("proxy_id = ?", nil).
		Where("proxy_id = ?", proxyId).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error) {
	var sms []*sqlModel
	err := dbtx.DB(ctx, r.db).NewSelect().
		Model(&sms).
		Where("proxy_id = ?", proxyId).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return r.toDomainModels(ctx, sms)
}

func (r *SQLRepositoryImpl) FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("push_token = ?", pushToken).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return r.toDomainModelWithProbes(ctx, sm)
}
