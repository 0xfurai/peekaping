package status_page

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:status_pages,alias:sp"`

	ID string `bun:"id,pk"`
	// WorkspaceID is set when the page is created and never changes
	WorkspaceID         string    `bun:"workspace_id,notnull"`
	Slug                string    `bun:"slug,unique,notnull"`
	Title               string    `bun:"title,notnull"`
	Description         string    `bun:"description"`
	Icon                string    `bun:"icon"`
	Theme               string    `bun:"theme,notnull,default:'light'"`
	Published           bool      `bun:"published,notnull,default:false"`
	ShowLocations       bool      `bun:"show_locations,notnull,default:false"`
	CreatedAt           time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt           time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	FooterText          string    `bun:"footer_text"`
	AutoRefreshInterval int       `bun:"auto_refresh_interval,notnull,default:30"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:                  sm.ID,
		WorkspaceID:         workspace.ID(sm.WorkspaceID),
		Title:               sm.Title,
		Description:         sm.Description,
		Slug:                sm.Slug,
		Icon:                sm.Icon,
		Theme:               sm.Theme,
		Published:           sm.Published,
		ShowLocations:       sm.ShowLocations,
		CreatedAt:           sm.CreatedAt,
		UpdatedAt:           sm.UpdatedAt,
		FooterText:          sm.FooterText,
		AutoRefreshInterval: sm.AutoRefreshInterval,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:                  m.ID,
		WorkspaceID:         string(m.WorkspaceID),
		Title:               m.Title,
		Description:         m.Description,
		Slug:                m.Slug,
		Icon:                m.Icon,
		Theme:               m.Theme,
		Published:           m.Published,
		ShowLocations:       m.ShowLocations,
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

func (r *SQLRepositoryImpl) Create(ctx context.Context, workspaceID workspace.ID, statusPage *Model) (*Model, error) {
	sm := toSQLModel(statusPage)
	sm.ID = uuid.New().String()
	sm.WorkspaceID = string(workspaceID) // whatever the page says
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return r.findOne(ctx, r.db.NewSelect().Where("id = ?", id).Where("workspace_id = ?", string(workspaceID)))
}

func (r *SQLRepositoryImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return r.findOne(ctx, r.db.NewSelect().Where("id = ?", id))
}

func (r *SQLRepositoryImpl) FindBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error) {
	return r.findOne(ctx, r.db.NewSelect().Where("slug = ?", slug))
}

func (r *SQLRepositoryImpl) findOne(ctx context.Context, query *bun.SelectQuery) (*Model, error) {
	sm := new(sqlModel)
	if err := query.Model(sm).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(
	ctx context.Context,
	workspaceID workspace.ID,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil)).Where("workspace_id = ?", string(workspaceID))

	if q != "" {
		query = query.Where("(LOWER(title) LIKE ? OR LOWER(description) LIKE ?)", "%"+q+"%", "%"+q+"%")
	}

	return r.list(ctx, query, page, limit)
}

func (r *SQLRepositoryImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return r.list(ctx, r.db.NewSelect().Model((*sqlModel)(nil)), page, limit)
}

func (r *SQLRepositoryImpl) list(ctx context.Context, query *bun.SelectQuery, page, limit int) ([]*Model, error) {
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

func (r *SQLRepositoryImpl) Update(ctx context.Context, workspaceID workspace.ID, id string, statusPage *UpdateModel) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID))

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
	if statusPage.ShowLocations != nil {
		query = query.Set("show_locations = ?", *statusPage.ShowLocations)
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
		found, err := r.FindByID(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if found == nil {
			return ErrNotFound
		}
		return nil
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	result, err := query.Exec(ctx)
	return requireOneRow(result, err)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	result, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID)).Exec(ctx)
	return requireOneRow(result, err)
}

// requireOneRow turns a write that changed no row into ErrNotFound: the page doesn't exist, or
// belongs to another workspace, and the caller can't tell which
func requireOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
