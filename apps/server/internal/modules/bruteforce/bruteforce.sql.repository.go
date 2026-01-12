package bruteforce

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// sqlModel represents the database model for login state
type sqlModel struct {
	bun.BaseModel `bun:"table:login_state,alias:ls"`

	Key         string     `bun:"key,pk"`
	FailCount   int        `bun:"fail_count,notnull"`
	FirstFailAt time.Time  `bun:"first_fail_at,notnull"`
	LockedUntil *time.Time `bun:"locked_until,nullzero"`
}

func toDomainModel(sm *sqlModel) *Model {
	return &Model{
		Key:         sm.Key,
		FailCount:   sm.FailCount,
		FirstFailAt: sm.FirstFailAt,
		LockedUntil: sm.LockedUntil,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		Key:         m.Key,
		FailCount:   m.FailCount,
		FirstFailAt: m.FirstFailAt,
		LockedUntil: m.LockedUntil,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

// FindByKey retrieves login state by key
func (r *SQLRepositoryImpl) FindByKey(ctx context.Context, key string) (*Model, error) {
	var sm sqlModel
	err := r.db.NewSelect().
		Model(&sm).
		Where("key = ?", key).
		Scan(ctx)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}

	return toDomainModel(&sm), nil
}

// Create creates a new login state record
func (r *SQLRepositoryImpl) Create(ctx context.Context, model *Model) (*Model, error) {
	sm := toSQLModel(model)
	_, err := r.db.NewInsert().Model(sm).Exec(ctx)
	if err != nil {
		return nil, err
	}
	return toDomainModel(sm), nil
}

// Update updates an existing login state record
func (r *SQLRepositoryImpl) Update(ctx context.Context, key string, updateModel *UpdateModel) error {
	query := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Where("key = ?", key)

	if updateModel.FailCount != nil {
		query = query.Set("fail_count = ?", *updateModel.FailCount)
	}
	if updateModel.FirstFailAt != nil {
		query = query.Set("first_fail_at = ?", *updateModel.FirstFailAt)
	}
	if updateModel.LockedUntil != nil {
		query = query.Set("locked_until = ?", *updateModel.LockedUntil)
	}

	_, err := query.Exec(ctx)
	return err
}

// Delete removes a login state record
func (r *SQLRepositoryImpl) Delete(ctx context.Context, key string) error {
	_, err := r.db.NewDelete().
		Model((*sqlModel)(nil)).
		Where("key = ?", key).
		Exec(ctx)
	return err
}

// IsLocked checks if a key is currently locked
func (r *SQLRepositoryImpl) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	var sm sqlModel
	err := r.db.NewSelect().
		Model(&sm).
		Where("key = ? AND locked_until > ?", key, time.Now()).
		Scan(ctx)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}

	if sm.LockedUntil != nil {
		return true, *sm.LockedUntil, nil
	}

	return false, time.Time{}, nil
}

// OnFailure atomically handles failure logic with window and locking
func (r *SQLRepositoryImpl) OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error) {
	var locked bool
	var until time.Time

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var sm sqlModel
		err := tx.NewSelect().
			Model(&sm).
			Where("key = ?", key).
			Scan(ctx)

		windowStart := now.Add(-window)

		if err != nil {
			if err.Error() == "sql: no rows in result set" {
				// First failure for this key
				sm = sqlModel{
					Key:         key,
					FailCount:   1,
					FirstFailAt: now,
					LockedUntil: nil,
				}
				_, err = tx.NewInsert().Model(&sm).Exec(ctx)
				return err
			}
			return err
		}

		// Check if we're outside the window - reset if so
		if sm.FirstFailAt.Before(windowStart) {
			sm.FailCount = 1
			sm.FirstFailAt = now
			sm.LockedUntil = nil
		} else {
			// Within window, increment counter
			sm.FailCount++

			// Check if we need to lock
			if sm.FailCount >= max {
				lockUntil := now.Add(lockout)
				sm.LockedUntil = &lockUntil
				locked = true
				until = lockUntil
			}
		}

		_, err = tx.NewUpdate().
			Model(&sm).
			Where("key = ?", key).
			Exec(ctx)

		return err
	})

	return locked, until, err
}
