package workspace

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:workspaces,alias:w"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:        sm.ID,
		Name:      sm.Name,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:        m.ID,
		Name:      m.Name,
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

func (r *SQLRepositoryImpl) Create(ctx context.Context, workspace *Model) (*Model, error) {
	sm := &sqlModel{
		ID:        uuid.New().String(),
		Name:      workspace.Name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

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

func (r *SQLRepositoryImpl) FindByIDs(ctx context.Context, workspaceIDs []string) ([]*Model, error) {
	if len(workspaceIDs) == 0 {
		return []*Model{}, nil
	}

	var sqlModels []sqlModel
	err := r.db.NewSelect().Model(&sqlModels).Where("id IN (?)", bun.In(workspaceIDs)).Scan(ctx)
	if err != nil {
		return nil, err
	}

	var workspaces []*Model
	for _, sm := range sqlModels {
		workspaces = append(workspaces, toDomainModelFromSQL(&sm))
	}

	return workspaces, nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int) ([]*Model, error) {
	var sqlModels []sqlModel
	offset := (page - 1) * limit

	err := r.db.NewSelect().Model(&sqlModels).Limit(limit).Offset(offset).Scan(ctx)
	if err != nil {
		return nil, err
	}

	var workspaces []*Model
	for _, sm := range sqlModels {
		workspaces = append(workspaces, toDomainModelFromSQL(&sm))
	}

	return workspaces, nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, workspace *UpdateModel) error {
	sm := &sqlModel{}

	if workspace.Name != nil {
		sm.Name = *workspace.Name
	}
	sm.UpdatedAt = time.Now()

	query := r.db.NewUpdate().Model(sm).Where("id = ?", id)

	if workspace.Name != nil {
		query = query.Set("name = ?", sm.Name)
	}
	query = query.Set("updated_at = ?", sm.UpdatedAt)

	_, err := query.Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}
