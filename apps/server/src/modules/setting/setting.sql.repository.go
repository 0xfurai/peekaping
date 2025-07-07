package setting

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:settings,alias:s"`

	Key       string    `bun:"key,pk"`
	Value     string    `bun:"value,notnull"`
	Type      string    `bun:"type,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		Key:       sm.Key,
		Value:     sm.Value,
		Type:      sm.Type,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) GetByKey(ctx context.Context, key string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("key = ?", key).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) SetByKey(ctx context.Context, key string, entity *CreateUpdateDto) (*Model, error) {
	// Try to update existing record first
	result, err := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Where("key = ?", key).
		Set("value = ?", entity.Value).
		Set("type = ?", entity.Type).
		Set("updated_at = ?", time.Now()).
		Exec(ctx)

	if err != nil {
		return nil, err
	}

	// Check if any rows were affected by the update
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	// If no rows were updated, insert a new record
	if rowsAffected == 0 {
		sm := &sqlModel{
			Key:       key,
			Value:     entity.Value,
			Type:      entity.Type,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = r.db.NewInsert().Model(sm).Exec(ctx)
		if err != nil {
			return nil, err
		}
	}

	// Fetch and return the final record
	sm := new(sqlModel)
	err = r.db.NewSelect().Model(sm).Where("key = ?", key).Scan(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) DeleteByKey(ctx context.Context, key string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("key = ?", key).Exec(ctx)
	return err
}
