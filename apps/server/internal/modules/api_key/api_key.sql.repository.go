package api_key

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
	bun.BaseModel `bun:"table:api_keys,alias:ak"`

	ID            string     `bun:"id,pk"`
	WorkspaceID   string     `bun:"workspace_id,notnull"`
	Name          string     `bun:"name,notnull"`
	KeyHash       string     `bun:"key_hash,notnull"`
	DisplayKey    string     `bun:"display_key,notnull"`
	LastUsed      *time.Time `bun:"last_used"`
	ExpiresAt     *time.Time `bun:"expires_at"`
	UsageCount    int64      `bun:"usage_count,notnull,default:0"`
	MaxUsageCount *int64     `bun:"max_usage_count"`
	CreatedAt     time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	// Handle missing display_key column gracefully
	displayKey := sm.DisplayKey
	if displayKey == "" {
		displayKey = ApiKeyPrefix + sm.ID[:6] + "..."
	}

	return &Model{
		ID:            sm.ID,
		WorkspaceID:   workspace.ID(sm.WorkspaceID),
		Name:          sm.Name,
		KeyHash:       sm.KeyHash,
		DisplayKey:    displayKey,
		LastUsed:      sm.LastUsed,
		ExpiresAt:     sm.ExpiresAt,
		UsageCount:    sm.UsageCount,
		MaxUsageCount: sm.MaxUsageCount,
		CreatedAt:     sm.CreatedAt,
		UpdatedAt:     sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:            m.ID,
		WorkspaceID:   string(m.WorkspaceID),
		Name:          m.Name,
		KeyHash:       m.KeyHash,
		DisplayKey:    m.DisplayKey,
		LastUsed:      m.LastUsed,
		ExpiresAt:     m.ExpiresAt,
		UsageCount:    m.UsageCount,
		MaxUsageCount: m.MaxUsageCount,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

// MARK: Constructor
func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

// MARK: Create
func (r *SQLRepositoryImpl) Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error) {
	// Generate API key ID upfront
	apiKeyID := uuid.New().String()

	sm := &sqlModel{
		ID:            apiKeyID,
		WorkspaceID:   string(apiKey.WorkspaceID),
		Name:          apiKey.Name,
		KeyHash:       apiKey.KeyHash,
		DisplayKey:    apiKey.DisplayKey,
		LastUsed:      nil,
		ExpiresAt:     apiKey.ExpiresAt,
		UsageCount:    0,
		MaxUsageCount: apiKey.MaxUsageCount,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	domainModel := toDomainModelFromSQL(sm)
	return &APIKeyWithToken{
		Model: *domainModel,
		// Token will be set by the service layer
	}, nil
}

// MARK: FindByID
func (r *SQLRepositoryImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return r.find(ctx, id, &workspaceID)
}

// MARK: FindByIDAnyWorkspace
func (r *SQLRepositoryImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return r.find(ctx, id, nil)
}

func (r *SQLRepositoryImpl) find(ctx context.Context, id string, workspaceID *workspace.ID) (*Model, error) {
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

// MARK: FindAll
func (r *SQLRepositoryImpl) FindAll(ctx context.Context, workspaceID workspace.ID) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().Model(&sms).Where("workspace_id = ?", string(workspaceID)).Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]*Model, len(sms))
	for i, sm := range sms {
		models[i] = toDomainModelFromSQL(sm)
	}
	return models, nil
}

// MARK: Update
func (r *SQLRepositoryImpl) Update(ctx context.Context, workspaceID workspace.ID, id string, update *UpdateModel) (*Model, error) {
	sm := new(sqlModel)

	// Build update query dynamically
	query := r.db.NewUpdate().Model(sm).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID))

	if update.Name != nil {
		query = query.Set("name = ?", *update.Name)
	}
	if update.ExpiresAt != nil {
		query = query.Set("expires_at = ?", *update.ExpiresAt)
	}
	if update.MaxUsageCount != nil {
		query = query.Set("max_usage_count = ?", *update.MaxUsageCount)
	}

	query = query.Set("updated_at = ?", time.Now())

	res, err := query.Returning("*").Exec(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := requireRow(res); err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

// MARK: Delete
func (r *SQLRepositoryImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	res, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Where("workspace_id = ?", string(workspaceID)).Exec(ctx)
	if err != nil {
		return err
	}
	return requireRow(res)
}

// requireRow is ErrNotFound when the statement matched no row: the key isn't in the workspace
func requireRow(res sql.Result) error {
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// MARK: ConsumeUsage
func (r *SQLRepositoryImpl) ConsumeUsage(ctx context.Context, id string) (bool, error) {
	// One conditional UPDATE checks the limit and counts the use, so concurrent
	// requests can't both take the last use.
	res, err := r.db.NewUpdate().Model((*sqlModel)(nil)).
		Set("last_used = ?", time.Now()).
		Set("usage_count = usage_count + 1").
		Where("id = ?", id).
		Where("(max_usage_count IS NULL OR usage_count < max_usage_count)").
		Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// MARK: UpdateKeyHash
func (r *SQLRepositoryImpl) UpdateKeyHash(ctx context.Context, id string, keyHash string, displayKey string) error {
	_, err := r.db.NewUpdate().Model((*sqlModel)(nil)).
		Set("key_hash = ?", keyHash).
		Set("display_key = ?", displayKey).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", id).
		Exec(ctx)
	return err
}
