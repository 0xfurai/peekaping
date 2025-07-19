package login_attempt

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type loginAttemptSQLModel struct {
	bun.BaseModel `bun:"table:login_attempts,alias:la"`

	ID          string    `bun:"id,pk"`
	Email       string    `bun:"email,notnull"`
	IPAddress   string    `bun:"ip_address,notnull"`
	UserAgent   string    `bun:"user_agent"`
	Success     bool      `bun:"success,notnull,default:false"`
	AttemptedAt time.Time `bun:"attempted_at,notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

func toLoginAttemptDomainModel(sm *loginAttemptSQLModel) *LoginAttempt {
	return &LoginAttempt{
		ID:          sm.ID,
		Email:       sm.Email,
		IPAddress:   sm.IPAddress,
		UserAgent:   sm.UserAgent,
		Success:     sm.Success,
		AttemptedAt: sm.AttemptedAt,
		CreatedAt:   sm.CreatedAt,
	}
}

func toLoginAttemptSQLModel(m *LoginAttempt) *loginAttemptSQLModel {
	return &loginAttemptSQLModel{
		ID:          m.ID,
		Email:       m.Email,
		IPAddress:   m.IPAddress,
		UserAgent:   m.UserAgent,
		Success:     m.Success,
		AttemptedAt: m.AttemptedAt,
		CreatedAt:   m.CreatedAt,
	}
}

type LoginAttemptSQLRepository struct {
	db *bun.DB
}

func NewLoginAttemptSQLRepository(db *bun.DB) LoginAttemptRepository {
	return &LoginAttemptSQLRepository{db: db}
}

func (r *LoginAttemptSQLRepository) Create(ctx context.Context, attempt *LoginAttemptCreateModel) (*LoginAttempt, error) {
	now := time.Now()
	sm := &loginAttemptSQLModel{
		ID:          uuid.New().String(),
		Email:       attempt.Email,
		IPAddress:   attempt.IPAddress,
		UserAgent:   attempt.UserAgent,
		Success:     attempt.Success,
		AttemptedAt: now,
		CreatedAt:   now,
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toLoginAttemptDomainModel(sm), nil
}

func (r *LoginAttemptSQLRepository) GetFailedAttemptsByEmail(ctx context.Context, email string, since time.Time) ([]*LoginAttempt, error) {
	var attempts []*loginAttemptSQLModel
	err := r.db.NewSelect().
		Model(&attempts).
		Where("email = ?", email).
		Where("success = ?", false).
		Where("attempted_at >= ?", since).
		Order("attempted_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	result := make([]*LoginAttempt, len(attempts))
	for i, attempt := range attempts {
		result[i] = toLoginAttemptDomainModel(attempt)
	}

	return result, nil
}

func (r *LoginAttemptSQLRepository) GetFailedAttemptsByIP(ctx context.Context, ipAddress string, since time.Time) ([]*LoginAttempt, error) {
	var attempts []*loginAttemptSQLModel
	err := r.db.NewSelect().
		Model(&attempts).
		Where("ip_address = ?", ipAddress).
		Where("success = ?", false).
		Where("attempted_at >= ?", since).
		Order("attempted_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	result := make([]*LoginAttempt, len(attempts))
	for i, attempt := range attempts {
		result[i] = toLoginAttemptDomainModel(attempt)
	}

	return result, nil
}

func (r *LoginAttemptSQLRepository) GetConsecutiveFailedAttemptsByEmail(ctx context.Context, email string) ([]*LoginAttempt, error) {
	var attempts []*loginAttemptSQLModel
	err := r.db.NewSelect().
		Model(&attempts).
		Where("email = ?", email).
		Order("attempted_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	// Find consecutive failed attempts from the most recent
	var consecutiveFailures []*LoginAttempt
	for _, attempt := range attempts {
		if attempt.Success {
			break // Stop at the first successful login
		}
		consecutiveFailures = append(consecutiveFailures, toLoginAttemptDomainModel(attempt))
	}

	return consecutiveFailures, nil
}

func (r *LoginAttemptSQLRepository) GetConsecutiveFailedAttemptsByIP(ctx context.Context, ipAddress string) ([]*LoginAttempt, error) {
	var attempts []*loginAttemptSQLModel
	err := r.db.NewSelect().
		Model(&attempts).
		Where("ip_address = ?", ipAddress).
		Order("attempted_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	// Find consecutive failed attempts from the most recent
	var consecutiveFailures []*LoginAttempt
	for _, attempt := range attempts {
		if attempt.Success {
			break // Stop at the first successful login
		}
		consecutiveFailures = append(consecutiveFailures, toLoginAttemptDomainModel(attempt))
	}

	return consecutiveFailures, nil
}

func (r *LoginAttemptSQLRepository) DeleteOldAttempts(ctx context.Context, olderThan time.Time) error {
	_, err := r.db.NewDelete().
		Model((*loginAttemptSQLModel)(nil)).
		Where("attempted_at < ?", olderThan).
		Exec(ctx)

	return err
}

func (r *LoginAttemptSQLRepository) GetLastSuccessfulLogin(ctx context.Context, email string) (*LoginAttempt, error) {
	var attempt loginAttemptSQLModel
	err := r.db.NewSelect().
		Model(&attempt).
		Where("email = ?", email).
		Where("success = ?", true).
		Order("attempted_at DESC").
		Limit(1).
		Scan(ctx)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}

	return toLoginAttemptDomainModel(&attempt), nil
}

func (r *LoginAttemptSQLRepository) GetAttemptsCount(ctx context.Context, email, ipAddress string, since time.Time) (int64, error) {
	query := r.db.NewSelect().
		Model((*loginAttemptSQLModel)(nil)).
		Where("attempted_at >= ?", since)

	if email != "" {
		query = query.Where("email = ?", email)
	}

	if ipAddress != "" {
		query = query.Where("ip_address = ?", ipAddress)
	}

	count, err := query.Count(ctx)
	return int64(count), err
}
