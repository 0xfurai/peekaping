package status_page

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:status_pages,alias:sp"`

	ID                  string    `bun:"id,pk"`
	OrgID               string    `bun:"org_id"`
	Slug                string    `bun:"slug,unique,notnull"`
	Title               string    `bun:"title,notnull"`
	Description         string    `bun:"description"`
	Icon                string    `bun:"icon"`
	Theme               string    `bun:"theme,notnull,default:'light'"`
	Published           bool      `bun:"published,notnull,default:false"`
	CreatedAt           time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt           time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	FooterText          string    `bun:"footer_text"`
	AutoRefreshInterval int       `bun:"auto_refresh_interval,notnull,default:30"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:                  sm.ID,
		OrgID:               sm.OrgID,
		Title:               sm.Title,
		Description:         sm.Description,
		Slug:                sm.Slug,
		Icon:                sm.Icon,
		Theme:               sm.Theme,
		Published:           sm.Published,
		CreatedAt:           sm.CreatedAt,
		UpdatedAt:           sm.UpdatedAt,
		FooterText:          sm.FooterText,
		AutoRefreshInterval: sm.AutoRefreshInterval,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:                  m.ID,
		OrgID:               m.OrgID,
		Title:               m.Title,
		Description:         m.Description,
		Slug:                m.Slug,
		Icon:                m.Icon,
		Theme:               m.Theme,
		Published:           m.Published,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
		FooterText:          m.FooterText,
		AutoRefreshInterval: m.AutoRefreshInterval,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, statusPage *Model) (*Model, error) {
	sm := toSQLModel(statusPage)
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
	query := r.db.NewSelect().Model(sm).Where("id = ?", id)

	if orgID != "" {
		query = query.Where("org_id = ?", orgID)
	}

	err := query.Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindBySlug(ctx context.Context, slug string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("slug = ?", slug).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	orgID string,
) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

	if orgID != "" {
		query = query.Where("org_id = ?", orgID)
	}

	if q != "" {
		query = query.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ?", "%"+q+"%", "%"+q+"%")
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

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, statusPage *UpdateModel, orgID string) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id).Where("org_id = ?", orgID)

	hasUpdates := false

	if statusPage.Title != nil {
		query = query.Set("title = ?", *statusPage.Title)
		hasUpdates = true
	}
	if statusPage.Description != nil {
		query = query.Set("description = ?", *statusPage.Description)
		hasUpdates = true
	}
	if statusPage.Slug != nil {
		query = query.Set("slug = ?", *statusPage.Slug)
		hasUpdates = true
	}
	if statusPage.Icon != nil {
		query = query.Set("icon = ?", *statusPage.Icon)
		hasUpdates = true
	}
	if statusPage.Theme != nil {
		query = query.Set("theme = ?", *statusPage.Theme)
		hasUpdates = true
	}
	if statusPage.Published != nil {
		query = query.Set("published = ?", *statusPage.Published)
		hasUpdates = true
	}
	if statusPage.FooterText != nil {
		query = query.Set("footer_text = ?", *statusPage.FooterText)
		hasUpdates = true
	}
	if statusPage.AutoRefreshInterval != nil {
		query = query.Set("auto_refresh_interval = ?", *statusPage.AutoRefreshInterval)
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
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Where("org_id = ?", orgID).Exec(ctx)
	return err
}
