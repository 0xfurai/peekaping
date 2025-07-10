package monitor

import (
	"context"
	"time"

	"peekaping/src/modules/shared"

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
	Active         bool                 `bun:"active,notnull,default:true"`
	Status         shared.MonitorStatus `bun:"status,notnull,default:0"`
	CreatedAt      time.Time            `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time            `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	Config         string               `bun:"config"`
	ProxyId        *string              `bun:"proxy_id"`
	PushToken      string               `bun:"push_token"`
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

func (r *SQLRepositoryImpl) FindByIDs(ctx context.Context, ids []string) ([]*Model, error) {
	if len(ids) == 0 {
		return []*Model{}, nil
	}

	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("id IN (?)", bun.In(ids)).
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

func (r *SQLRepositoryImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	active *bool,
	status *int,
) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

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

	query = query.Order("created_at DESC").
		Limit(limit).
		Offset(page * limit)

	var sms []*sqlModel
	err := query.Scan(ctx, &sms)
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
		Where("active = ?", true).
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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, monitor *Model) error {
	sm := toSQLModel(monitor)
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		ExcludeColumn("id", "created_at").
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, monitor *UpdateModel) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

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

	if !hasUpdates {
		return nil
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	_, err := query.Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) RemoveProxyReference(ctx context.Context, proxyId string) error {
	_, err := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Set("proxy_id = ?", nil).
		Where("proxy_id = ?", proxyId).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("proxy_id = ?", proxyId).
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

func (r *SQLRepositoryImpl) FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("push_token = ?", pushToken).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}
