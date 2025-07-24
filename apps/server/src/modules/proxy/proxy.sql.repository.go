package proxy

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:proxies,alias:p"`

	ID        string    `bun:"id,pk"`
	Protocol  string    `bun:"protocol,notnull"`
	Host      string    `bun:"host,notnull"`
	Port      int       `bun:"port,notnull"`
	Auth      bool      `bun:"auth,notnull,default:false"`
	Username  string    `bun:"username"`
	Password  string    `bun:"password"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:        sm.ID,
		Protocol:  sm.Protocol,
		Host:      sm.Host,
		Port:      sm.Port,
		Auth:      sm.Auth,
		Username:  sm.Username,
		Password:  sm.Password,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:        m.ID,
		Protocol:  m.Protocol,
		Host:      m.Host,
		Port:      m.Port,
		Auth:      m.Auth,
		Username:  m.Username,
		Password:  m.Password,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	sm := toSQLModel(entity)
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

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

	if q != "" {
		query = query.Where("LOWER(host) LIKE ?", "%"+q+"%")
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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error) {
	sm := toSQLModel(entity)
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		OmitZero().
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error) {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

	hasUpdates := false

	if entity.Protocol != nil {
		query = query.Set("protocol = ?", *entity.Protocol)
		hasUpdates = true
	}
	if entity.Host != nil {
		query = query.Set("host = ?", *entity.Host)
		hasUpdates = true
	}
	if entity.Port != nil {
		query = query.Set("port = ?", *entity.Port)
		hasUpdates = true
	}
	if entity.Auth != nil {
		query = query.Set("auth = ?", *entity.Auth)
		hasUpdates = true
	}
	if entity.Username != nil {
		query = query.Set("username = ?", *entity.Username)
		hasUpdates = true
	}
	if entity.Password != nil {
		query = query.Set("password = ?", *entity.Password)
		hasUpdates = true
	}

	if !hasUpdates {
		return r.FindByID(ctx, id)
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	_, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}
