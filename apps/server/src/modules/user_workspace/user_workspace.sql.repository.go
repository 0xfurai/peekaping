package user_workspace

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:user_workspace,alias:uw"`

	UserID      string    `bun:"user_id,pk"`
	WorkspaceID string    `bun:"workspace_id,pk"`
	Role        string    `bun:"role,notnull,default:'member'"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		UserID:      sm.UserID,
		WorkspaceID: sm.WorkspaceID,
		Role:        sm.Role,
		CreatedAt:   sm.CreatedAt,
		UpdatedAt:   sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		UserID:      m.UserID,
		WorkspaceID: m.WorkspaceID,
		Role:        m.Role,
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

func (r *SQLRepositoryImpl) Create(ctx context.Context, userWorkspace *Model) (*Model, error) {
	sm := &sqlModel{
		UserID:      userWorkspace.UserID,
		WorkspaceID: userWorkspace.WorkspaceID,
		Role:        userWorkspace.Role,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	var sqlModels []sqlModel
	err := r.db.NewSelect().Model(&sqlModels).Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		return nil, err
	}

	var userWorkspaces []*Model
	for _, sm := range sqlModels {
		userWorkspaces = append(userWorkspaces, toDomainModelFromSQL(&sm))
	}

	return userWorkspaces, nil
}

func (r *SQLRepositoryImpl) FindByWorkspaceID(ctx context.Context, workspaceID string) ([]*Model, error) {
	var sqlModels []sqlModel
	err := r.db.NewSelect().Model(&sqlModels).Where("workspace_id = ?", workspaceID).Scan(ctx)
	if err != nil {
		return nil, err
	}

	var userWorkspaces []*Model
	for _, sm := range sqlModels {
		userWorkspaces = append(userWorkspaces, toDomainModelFromSQL(&sm))
	}

	return userWorkspaces, nil
}

func (r *SQLRepositoryImpl) FindByUserAndWorkspace(ctx context.Context, userID, workspaceID string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, userID, workspaceID string, userWorkspace *UpdateModel) error {
	sm := &sqlModel{}

	if userWorkspace.Role != nil {
		sm.Role = *userWorkspace.Role
	}
	sm.UpdatedAt = time.Now()

	query := r.db.NewUpdate().Model(sm).Where("user_id = ? AND workspace_id = ?", userID, workspaceID)

	if userWorkspace.Role != nil {
		query = query.Set("role = ?", sm.Role)
	}
	query = query.Set("updated_at = ?", sm.UpdatedAt)

	_, err := query.Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, userID, workspaceID string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Exec(ctx)
	return err
}
