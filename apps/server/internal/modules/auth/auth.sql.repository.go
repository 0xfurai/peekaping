package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID             string `bun:"id,pk"`
	Email          string `bun:"email,unique,notnull"`
	Password       string `bun:"password,notnull"`
	Active         bool   `bun:"active,notnull,default:true"`
	TwoFASecret    string `bun:"twofa_secret"`
	TwoFAStatus    bool   `bun:"twofa_status,notnull,default:false"`
	TwoFALastToken string `bun:"twofa_last_token"`
	InstanceAdmin  bool   `bun:"instance_admin,notnull,default:false"`

	EmailVerifiedAt  *time.Time `bun:"email_verified_at"`
	TokensValidAfter *time.Time `bun:"tokens_valid_after"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:             sm.ID,
		Email:          sm.Email,
		Password:       sm.Password,
		Active:         sm.Active,
		TwoFASecret:    sm.TwoFASecret,
		TwoFAStatus:    sm.TwoFAStatus,
		TwoFALastToken: sm.TwoFALastToken,
		InstanceAdmin:  sm.InstanceAdmin,

		EmailVerifiedAt:  sm.EmailVerifiedAt,
		TokensValidAfter: sm.TokensValidAfter,

		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	return &sqlModel{
		ID:             m.ID,
		Email:          m.Email,
		Password:       m.Password,
		Active:         m.Active,
		TwoFASecret:    m.TwoFASecret,
		TwoFAStatus:    m.TwoFAStatus,
		TwoFALastToken: m.TwoFALastToken,
		InstanceAdmin:  m.InstanceAdmin,

		EmailVerifiedAt:  m.EmailVerifiedAt,
		TokensValidAfter: m.TokensValidAfter,

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

func (r *SQLRepositoryImpl) Create(ctx context.Context, user *Model) (*Model, error) {
	sm := &sqlModel{
		ID:             uuid.New().String(),
		Email:          user.Email,
		Password:       user.Password,
		Active:         user.Active,
		TwoFASecret:    user.TwoFASecret,
		TwoFAStatus:    user.TwoFAStatus,
		TwoFALastToken: user.TwoFALastToken,
		InstanceAdmin:  user.InstanceAdmin,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Let Bun handle ID generation based on the database type. The insert joins the transaction the
	// context carries, so that signup through an invite creates the user and the membership together.
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

// firstAdminLockKey is the PostgreSQL advisory lock that CreateFirst takes. Any
// number would do; this one is just not used for anything else.
const firstAdminLockKey = 0x70656b6166697273

func (r *SQLRepositoryImpl) CreateFirst(ctx context.Context, user *Model) (*Model, error) {
	sm := &sqlModel{
		ID:             uuid.New().String(),
		Email:          user.Email,
		Password:       user.Password,
		Active:         user.Active,
		TwoFASecret:    user.TwoFASecret,
		TwoFAStatus:    user.TwoFAStatus,
		TwoFALastToken: user.TwoFALastToken,
		InstanceAdmin:  true, // the first user manages the install
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// One statement inserts the user only while the table is empty. SQLite
	// serialises its writes, so that statement alone is atomic there.
	insert := func(db bun.IDB) error {
		res, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password, active, twofa_secret, twofa_status, twofa_last_token, instance_admin, created_at, updated_at)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
			sm.ID, sm.Email, sm.Password, sm.Active, sm.TwoFASecret, sm.TwoFAStatus, sm.TwoFALastToken, sm.InstanceAdmin, sm.CreatedAt, sm.UpdatedAt)
		if err != nil {
			return err
		}
		inserted, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if inserted == 0 {
			return ErrAdminExists
		}
		return nil
	}

	var err error
	switch handle := dbtx.DB(ctx, r.db).(type) {
	case bun.Tx:
		// The caller's transaction ends the lock, so what the caller writes after the user, such
		// as the user's workspace, is committed before the next call gets the lock
		err = r.createFirstIn(ctx, handle, insert)
	default:
		if r.db.Dialect().Name() == dialect.PG {
			err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				return r.createFirstIn(ctx, tx, insert)
			})
		} else {
			err = insert(r.db)
		}
	}
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

// createFirstIn inserts the user in tx. Two PostgreSQL transactions can both see an empty table,
// so they take turns: the lock is held until the transaction ends, and the call that gets it
// next sees the user the one before it created.
func (r *SQLRepositoryImpl) createFirstIn(ctx context.Context, tx bun.Tx, insert func(bun.IDB) error) error {
	if r.db.Dialect().Name() == dialect.PG {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", firstAdminLockKey); err != nil {
			return err
		}
	}
	return insert(tx)
}

func (r *SQLRepositoryImpl) FindByEmail(ctx context.Context, email string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("email = ?", email).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByEmailIgnoringCase(ctx context.Context, email string) (*Model, error) {
	trimmed := strings.TrimSpace(email)
	sm := new(sqlModel)
	// The exact match covers what SQL's lower() leaves alone (it differs from Go's for non-ASCII
	// letters on SQLite)
	err := r.db.NewSelect().Model(sm).
		Where("email = ? OR lower(trim(email)) = ?", trimmed, strings.ToLower(trimmed)).
		Order("created_at", "id").Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
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

func (r *SQLRepositoryImpl) FindAllCount(ctx context.Context) (int64, error) {
	count, err := r.db.NewSelect().Model((*sqlModel)(nil)).Count(ctx)
	return int64(count), err
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, entity *UpdateModel) error {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

	hasUpdates := false

	if entity.Email != nil {
		query = query.Set("email = ?", *entity.Email)
		hasUpdates = true
	}
	if entity.Password != nil {
		query = query.Set("password = ?", *entity.Password)
		hasUpdates = true
	}
	if entity.Active != nil {
		query = query.Set("active = ?", *entity.Active)
		hasUpdates = true
	}
	if entity.TwoFASecret != nil {
		query = query.Set("twofa_secret = ?", *entity.TwoFASecret)
		hasUpdates = true
	}
	if entity.TwoFAStatus != nil {
		query = query.Set("twofa_status = ?", *entity.TwoFAStatus)
		hasUpdates = true
	}
	if entity.TwoFALastToken != nil {
		query = query.Set("twofa_last_token = ?", *entity.TwoFALastToken)
		hasUpdates = true
	}

	if !hasUpdates {
		return nil
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	_, err := query.Exec(ctx)
	return err
}
