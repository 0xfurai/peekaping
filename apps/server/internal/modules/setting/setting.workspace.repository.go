package setting

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace"

	"github.com/uptrace/bun"
)

// WorkspaceRepository stores the settings of a workspace. A setting is read and written by its
// workspace and key.
type WorkspaceRepository interface {
	GetByKey(ctx context.Context, workspaceID workspace.ID, key string) (*Model, error)
	// SetByKey stores entity under the key, replacing what the workspace holds there
	SetByKey(ctx context.Context, workspaceID workspace.ID, key string, entity *Model) (*Model, error)
	DeleteByKey(ctx context.Context, workspaceID workspace.ID, key string) error
}

type sqlWorkspaceModel struct {
	bun.BaseModel `bun:"table:workspace_settings,alias:ws"`

	WorkspaceID string    `bun:"workspace_id,pk"`
	Key         string    `bun:"key,pk"`
	Value       string    `bun:"value,notnull"`
	Type        string    `bun:"type,notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func (m *sqlWorkspaceModel) toDomain() *Model {
	return &Model{Key: m.Key, Value: m.Value, Type: m.Type, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

type SQLWorkspaceRepositoryImpl struct {
	db *bun.DB
}

func NewSQLWorkspaceRepository(db *bun.DB) WorkspaceRepository {
	return &SQLWorkspaceRepositoryImpl{db: db}
}

func (r *SQLWorkspaceRepositoryImpl) GetByKey(ctx context.Context, workspaceID workspace.ID, key string) (*Model, error) {
	sm := new(sqlWorkspaceModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("workspace_id = ?", string(workspaceID)).Where("key = ?", key).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sm.toDomain(), nil
}

func (r *SQLWorkspaceRepositoryImpl) SetByKey(ctx context.Context, workspaceID workspace.ID, key string, entity *Model) (*Model, error) {
	now := time.Now().UTC()
	sm := &sqlWorkspaceModel{WorkspaceID: string(workspaceID), Key: key, Value: entity.Value, Type: entity.Type, CreatedAt: now, UpdatedAt: now}
	// One statement, so two writers of the same key can't both miss the row and insert it
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).
		On("CONFLICT (workspace_id, key) DO UPDATE").
		Set("value = EXCLUDED.value").
		Set("type = EXCLUDED.type").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return r.GetByKey(ctx, workspaceID, key)
}

func (r *SQLWorkspaceRepositoryImpl) DeleteByKey(ctx context.Context, workspaceID workspace.ID, key string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlWorkspaceModel)(nil)).Where("workspace_id = ?", string(workspaceID)).Where("key = ?", key).Exec(ctx)
	return err
}
