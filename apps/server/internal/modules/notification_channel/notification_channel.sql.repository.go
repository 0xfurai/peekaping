package notification_channel

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
	bun.BaseModel `bun:"table:notification_channels,alias:nc"`

	ID string `bun:"id,pk"`
	// WorkspaceID is set when the channel is created and never changes
	WorkspaceID string    `bun:"workspace_id,notnull"`
	Name        string    `bun:"name,notnull"`
	Type        string    `bun:"type,notnull"`
	Active      bool      `bun:"active,notnull"` // no Bun default: it would replace false with the column default
	IsDefault   bool      `bun:"is_default,notnull,default:false"`
	Config      *string   `bun:"config"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:          sm.ID,
		WorkspaceID: workspace.ID(sm.WorkspaceID),
		Name:        sm.Name,
		Type:        sm.Type,
		Active:      sm.Active,
		IsDefault:   sm.IsDefault,
		Config:      sm.Config,
		CreatedAt:   sm.CreatedAt,
		UpdatedAt:   sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:          m.ID,
		WorkspaceID: string(m.WorkspaceID),
		Name:        m.Name,
		Type:        m.Type,
		Active:      m.Active,
		IsDefault:   m.IsDefault,
		Config:      m.Config,
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
	sm.WorkspaceID = string(workspaceID) // whatever the channel says
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return r.find(ctx, &workspaceID, id)
}

func (r *SQLRepositoryImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return r.find(ctx, nil, id)
}

// find reads the channel in the workspace, or across workspaces when there is none
func (r *SQLRepositoryImpl) find(ctx context.Context, workspaceID *workspace.ID, id string) (*Model, error) {
	sm := new(sqlModel)
	query := r.db.NewSelect().Model(sm).Where("id = ?", id)
	if workspaceID != nil {
		query = query.Where("workspace_id = ?", string(*workspaceID))
	}
	if err := query.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error) {
	return r.findAll(ctx, &workspaceID, page, limit, q)
}

func (r *SQLRepositoryImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return r.findAll(ctx, nil, page, limit, "")
}

func (r *SQLRepositoryImpl) findAll(ctx context.Context, workspaceID *workspace.ID, page int, limit int, q string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

	if workspaceID != nil {
		query = query.Where("workspace_id = ?", string(*workspaceID))
	}
	if q != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+q+"%")
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

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) error {
	sm := toSQLModel(entity)
	sm.UpdatedAt = time.Now()

	result, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		Where("workspace_id = ?", string(workspaceID)).
		ExcludeColumn("id", "workspace_id", "created_at").
		OmitZero().
		Exec(ctx)
	return requireOneRow(result, err)
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID))

	hasUpdates := false

	if entity.Name != nil {
		query = query.Set("name = ?", *entity.Name)
		hasUpdates = true
	}
	if entity.Type != nil {
		query = query.Set("type = ?", *entity.Type)
		hasUpdates = true
	}
	if entity.Active != nil {
		query = query.Set("active = ?", *entity.Active)
		hasUpdates = true
	}
	if entity.IsDefault != nil {
		query = query.Set("is_default = ?", *entity.IsDefault)
		hasUpdates = true
	}
	if entity.Config != nil {
		query = query.Set("config = ?", *entity.Config)
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

// requireOneRow turns a write that changed no row into ErrNotFound: the channel doesn't exist, or
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
