package tag

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:tags,alias:t"`

	ID string `bun:"id,pk"`
	// WorkspaceID is set when the tag is created and never changes
	WorkspaceID string `bun:"workspace_id,notnull"`
	// Name is unique within a workspace: the database holds the rule in an index of
	// (workspace_id, name)
	Name        string    `bun:"name,notnull"`
	Color       string    `bun:"color,notnull"`
	Description *string   `bun:"description"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:          sm.ID,
		WorkspaceID: workspace.ID(sm.WorkspaceID),
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
		WorkspaceID: string(m.WorkspaceID),
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

func (r *SQLRepositoryImpl) Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error) {
	sm := toSQLModel(entity)
	sm.ID = uuid.New().String()
	sm.WorkspaceID = string(workspaceID) // whatever the tag says
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, nameTaken(err)
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return r.findOne(ctx, dbtx.DB(ctx, r.db).NewSelect().Where("id = ?", id), workspaceID)
}

func (r *SQLRepositoryImpl) FindByName(ctx context.Context, workspaceID workspace.ID, name string) (*Model, error) {
	return r.findOne(ctx, dbtx.DB(ctx, r.db).NewSelect().Where("name = ?", name), workspaceID)
}

func (r *SQLRepositoryImpl) findOne(ctx context.Context, query *bun.SelectQuery, workspaceID workspace.ID) (*Model, error) {
	sm := new(sqlModel)
	if err := query.Model(sm).Where("workspace_id = ?", string(workspaceID)).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error) {
	query := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil)).Where("workspace_id = ?", string(workspaceID))

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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) error {
	sm := toSQLModel(entity)
	sm.UpdatedAt = time.Now()

	result, err := dbtx.DB(ctx, r.db).NewUpdate().
		Model(sm).
		Where("id = ?", id).
		Where("workspace_id = ?", string(workspaceID)).
		ExcludeColumn("id", "workspace_id", "created_at").
		Exec(ctx)
	return requireOneRow(result, nameTaken(err))
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error {
	query := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID))

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
	return requireOneRow(result, nameTaken(err))
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	result, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID)).Exec(ctx)
	return requireOneRow(result, err)
}

// nameTaken turns the database's refusal of a name the workspace has already, which a request that
// ran between the service's check and the write can cause, into the answer the check gives
func nameTaken(err error) error {
	if dbtx.IsUniqueViolation(err) {
		return ErrNameTaken
	}
	return err
}

// requireOneRow turns a write that changed no row into ErrNotFound: the tag doesn't exist, or
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
