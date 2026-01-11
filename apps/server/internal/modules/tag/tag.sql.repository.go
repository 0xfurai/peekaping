package tag

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:tags,alias:t"`

	ID          string    `bun:"id,pk"`
	OrgID       string    `bun:"org_id,notnull"`
	Name        string    `bun:"name,notnull"`
	Color       string    `bun:"color,notnull"`
	Description *string   `bun:"description"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:          sm.ID,
		OrgID:       sm.OrgID,
		Name:        sm.Name,
		Color:       sm.Color,
		Description: sm.Description,
		CreatedAt:   sm.CreatedAt,
		UpdatedAt:   sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:          m.ID,
		OrgID:       m.OrgID,
		Name:        m.Name,
		Color:       m.Color,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
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

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("id = ? AND org_id = ?", id, orgID).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByName(ctx context.Context, name string, orgID string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("name = ? AND org_id = ?", name, orgID).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil)).Where("org_id = ?", orgID)

	if q != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+q+"%")
	}

	query = query.Order("name ASC").
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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model) error {
	sm := toSQLModel(entity)
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ? AND org_id = ?", id, entity.OrgID).
		ExcludeColumn("id", "created_at").
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ? AND org_id = ?", id, entity.OrgID)

	hasUpdates := false

	if entity.Name != nil {
		query = query.Set("name = ?", *entity.Name)
		hasUpdates = true
	}
	if entity.Color != nil {
		query = query.Set("color = ?", *entity.Color)
		hasUpdates = true
	}
	if entity.Description != nil {
		query = query.Set("description = ?", *entity.Description)
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

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	sm := &sqlModel{ID: id}
	_, err := r.db.NewDelete().Model(sm).WherePK().Where("org_id = ?", orgID).Exec(ctx)
	return err
}
