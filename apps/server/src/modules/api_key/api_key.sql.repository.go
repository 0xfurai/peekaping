package api_key

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:api_keys,alias:ak"`

	ID             string     `bun:"id,pk"`
	UserID         string     `bun:"user_id,notnull"`
	Name           string     `bun:"name,notnull"`
	KeyHash        string     `bun:"key_hash,notnull"`
	DisplayKey     string     `bun:"display_key,notnull"`
	LastUsed       *time.Time `bun:"last_used"`
	ExpiresAt      *time.Time `bun:"expires_at"`
	UsageCount     int64      `bun:"usage_count,notnull,default:0"`
	MaxUsageCount  *int64     `bun:"max_usage_count"`
	CreatedAt      time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	// Handle missing display_key column gracefully
	displayKey := sm.DisplayKey
	if displayKey == "" {
		displayKey = "pk_" + sm.ID[:6] + "..."
	}
	
	return &Model{
		ID:            sm.ID,
		UserID:        sm.UserID,
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
		UserID:        m.UserID,
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

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error) {
	// Generate a secure API key
	token, keyHash, displayKey, err := generateAPIKey()
	if err != nil {
		return nil, err
	}

	sm := &sqlModel{
		ID:            uuid.New().String(),
		UserID:        apiKey.UserID,
		Name:          apiKey.Name,
		KeyHash:       keyHash,
		DisplayKey:    displayKey,
		LastUsed:      nil,
		ExpiresAt:     apiKey.ExpiresAt,
		UsageCount:    0,
		MaxUsageCount: apiKey.MaxUsageCount,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	_, err = r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	domainModel := toDomainModelFromSQL(sm)
	return &APIKeyWithToken{
		Model: *domainModel,
		Token: token,
	}, nil
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

func (r *SQLRepositoryImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	var sms []*sqlModel
	err := r.db.NewSelect().Model(&sms).Where("user_id = ?", userID).Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]*Model, len(sms))
	for i, sm := range sms {
		models[i] = toDomainModelFromSQL(sm)
	}
	return models, nil
}

func (r *SQLRepositoryImpl) FindByKeyHash(ctx context.Context, keyHash string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("key_hash = ?", keyHash).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, update *UpdateModel) (*Model, error) {
	sm := new(sqlModel)
	
	// Build update query dynamically
	query := r.db.NewUpdate().Model(sm).Where("id = ?", id)
	
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
	
	_, err := query.Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}
	
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context) ([]*Model, error) {
	var sqlModels []sqlModel
	err := r.db.NewSelect().Model(&sqlModels).Scan(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]*Model, len(sqlModels))
	for i, sqlModel := range sqlModels {
		models[i] = toDomainModelFromSQL(&sqlModel)
	}

	return models, nil
}

func (r *SQLRepositoryImpl) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.db.NewUpdate().Model((*sqlModel)(nil)).
		Set("last_used = ?", time.Now()).
		Set("usage_count = usage_count + 1").
		Where("id = ?", id).
		Exec(ctx)
	return err
}
