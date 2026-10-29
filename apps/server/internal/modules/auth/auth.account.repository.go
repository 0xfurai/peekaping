package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"

	"github.com/uptrace/bun"
)

// The purposes of an account token
const (
	PurposeReset  = "reset"
	PurposeVerify = "verify"
)

// AccountToken is a single-use link in an email: to reset a password or to verify an address. The
// token itself is shown once, in the email; the record holds its hash.
type AccountToken struct {
	ID        string
	UserID    string
	Purpose   string
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// AccountRepository stores what the account emails need: their tokens, and the parts of a user
// that a reset or a verification changes. Every method joins the transaction its ctx carries
// (see internal/dbtx).
type AccountRepository interface {
	CreateToken(ctx context.Context, token *AccountToken) error
	// FindLiveToken returns the ID of the user of the token with the hash and purpose, if it is
	// neither used nor expired at now, and "" when there is no such token. It changes nothing: a
	// caller that wants the token spent uses ConsumeToken, which stays the guard.
	FindLiveToken(ctx context.Context, purpose, tokenHash string, now time.Time) (userID string, err error)
	// ConsumeToken marks the token with the hash and purpose as used, if it is neither used nor
	// expired at now, and returns the ID of its user. It returns "" when there is no such token.
	// The check and the mark are one statement, so a link works once however many requests use it
	// at the same time.
	ConsumeToken(ctx context.Context, purpose, tokenHash string, now time.Time) (userID string, err error)
	// DeleteUnusedTokens deletes the user's tokens of the purpose that were not used
	DeleteUnusedTokens(ctx context.Context, userID, purpose string) error
	// DeleteSpentTokens deletes the tokens that were used before the cutoff, and those that expired
	// before it, and returns how many. A token that still works, or ended after the cutoff, stays.
	DeleteSpentTokens(ctx context.Context, before time.Time) (int64, error)
	// SetPassword sets the user's password hash and ends every session issued before at
	SetPassword(ctx context.Context, userID, passwordHash string, at time.Time) error
	// MarkEmailVerified records that the user's address was verified at the time, unless it was
	// verified already: the first time stays
	MarkEmailVerified(ctx context.Context, userID string, at time.Time) error
	// TokensValidAfter returns when the user's earlier sessions were ended, nil for none, and for a
	// user that doesn't exist. It is a SessionCutoffs.
	TokensValidAfter(ctx context.Context, userID string) (*time.Time, error)
}

type accountTokenModel struct {
	bun.BaseModel `bun:"table:account_tokens,alias:t"`

	ID        string     `bun:"id,pk"`
	UserID    string     `bun:"user_id,notnull"`
	Purpose   string     `bun:"purpose,notnull"`
	TokenHash string     `bun:"token_hash,notnull"`
	CreatedAt time.Time  `bun:"created_at,notnull"`
	ExpiresAt time.Time  `bun:"expires_at,notnull"`
	UsedAt    *time.Time `bun:"used_at"`
}

type SQLAccountRepository struct {
	db *bun.DB
}

func NewSQLAccountRepository(db *bun.DB) AccountRepository {
	return &SQLAccountRepository{db: db}
}

func (r *SQLAccountRepository) CreateToken(ctx context.Context, token *AccountToken) error {
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(&accountTokenModel{
		ID: token.ID, UserID: token.UserID, Purpose: token.Purpose, TokenHash: token.TokenHash,
		CreatedAt: token.CreatedAt.UTC(), ExpiresAt: token.ExpiresAt.UTC(),
	}).Exec(ctx)
	return err
}

func (r *SQLAccountRepository) FindLiveToken(ctx context.Context, purpose, tokenHash string, now time.Time) (string, error) {
	var userID string
	err := dbtx.DB(ctx, r.db).NewSelect().Model((*accountTokenModel)(nil)).Column("user_id").
		Where("token_hash = ?", tokenHash).
		Where("purpose = ?", purpose).
		Where("used_at IS NULL").
		Where("expires_at > ?", now.UTC()).
		Scan(ctx, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (r *SQLAccountRepository) ConsumeToken(ctx context.Context, purpose, tokenHash string, now time.Time) (string, error) {
	var userID string
	err := dbtx.DB(ctx, r.db).NewUpdate().Model((*accountTokenModel)(nil)).
		Set("used_at = ?", now.UTC()).
		Where("token_hash = ?", tokenHash).
		Where("purpose = ?", purpose).
		Where("used_at IS NULL").
		Where("expires_at > ?", now.UTC()).
		Returning("user_id").
		Scan(ctx, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (r *SQLAccountRepository) DeleteUnusedTokens(ctx context.Context, userID, purpose string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().Model((*accountTokenModel)(nil)).
		Where("user_id = ?", userID).Where("purpose = ?", purpose).Where("used_at IS NULL").Exec(ctx)
	return err
}

func (r *SQLAccountRepository) DeleteSpentTokens(ctx context.Context, before time.Time) (int64, error) {
	res, err := dbtx.DB(ctx, r.db).NewDelete().Model((*accountTokenModel)(nil)).
		WhereGroup(" AND ", func(q *bun.DeleteQuery) *bun.DeleteQuery {
			return q.Where("used_at < ?", before.UTC()).WhereOr("expires_at < ?", before.UTC())
		}).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *SQLAccountRepository) SetPassword(ctx context.Context, userID, passwordHash string, at time.Time) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).
		Set("password = ?", passwordHash).
		Set("tokens_valid_after = ?", at.UTC()).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", userID).Exec(ctx)
	return err
}

func (r *SQLAccountRepository) MarkEmailVerified(ctx context.Context, userID string, at time.Time) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).
		Set("email_verified_at = ?", at.UTC()).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", userID).Where("email_verified_at IS NULL").Exec(ctx)
	return err
}

func (r *SQLAccountRepository) TokensValidAfter(ctx context.Context, userID string) (*time.Time, error) {
	var after *time.Time
	err := dbtx.DB(ctx, r.db).NewSelect().Model((*sqlModel)(nil)).Column("tokens_valid_after").Where("id = ?", userID).Scan(ctx, &after)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if after != nil {
		utc := after.UTC()
		after = &utc
	}
	return after, nil
}
