package auth_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/auth"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// PP-152: the hourly cleanup deletes the account tokens that are used or expired for more than a
// day, and leaves the ones that still work and the ones that ended recently.
func TestAccountRepository_DeleteSpentTokensDeletesTheUsedAndExpiredForMoreThanADay(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			userID := uuid.NewString()
			_, err = db.Exec("INSERT INTO users (id, email, password) VALUES (?, 'u@example.com', 'hash')", userID)
			require.NoError(t, err)

			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			ago := func(d time.Duration) time.Time { return now.Add(-d) }
			insert := func(label string, expiresAt time.Time, usedAt *time.Time) {
				_, err := db.Exec("INSERT INTO account_tokens (id, user_id, purpose, token_hash, created_at, expires_at, used_at) VALUES (?, ?, 'reset', ?, ?, ?, ?)",
					uuid.NewString(), userID, label, ago(100*time.Hour), expiresAt.UTC(), usedAt)
				require.NoError(t, err)
			}
			usedLongAgo, usedRecently := ago(30*time.Hour), ago(2*time.Hour)
			insert("live", now.Add(time.Hour), nil)
			insert("expired-recently", ago(2*time.Hour), nil)
			insert("expired-long-ago", ago(30*time.Hour), nil)
			insert("used-long-ago", now.Add(time.Hour), &usedLongAgo)
			insert("used-recently", now.Add(time.Hour), &usedRecently)

			deleted, err := auth.NewSQLAccountRepository(db).DeleteSpentTokens(ctx, now.Add(-24*time.Hour))

			require.NoError(t, err)
			assert.EqualValues(t, 2, deleted)
			var left []string
			require.NoError(t, db.NewSelect().Table("account_tokens").Column("token_hash").Order("token_hash").Scan(ctx, &left))
			assert.Equal(t, []string{"expired-recently", "live", "used-recently"}, left)
		})
	}
}
