package auth_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// accountEmailsMigration adds the account tokens and the verification columns (PP-152)
const accountEmailsMigration = "20261108120000"

type verification struct {
	Email            string     `bun:"email"`
	EmailVerifiedAt  *time.Time `bun:"email_verified_at"`
	TokensValidAfter *time.Time `bun:"tokens_valid_after"`
}

func usersOf(t *testing.T, db *bun.DB) map[string]verification {
	t.Helper()
	var rows []verification
	require.NoError(t, db.NewSelect().Table("users").Column("email", "email_verified_at", "tokens_valid_after").Scan(context.Background(), &rows))
	out := map[string]verification{}
	for _, r := range rows {
		out[r.Email] = r
	}
	return out
}

// An install that upgrades keeps its users as they are: they count as verified, and no session is
// ended. A user who signs up afterwards is not verified (PP-152).
func TestMigration_AccountEmails_ExistingUsersCountAsVerified(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < accountEmailsMigration {
					before.Add(m)
				}
			}
			old := migrate.NewMigrator(db, before)
			require.NoError(t, old.Init(ctx))
			_, err := old.Migrate(ctx)
			require.NoError(t, err)
			for _, email := range []string{"first@example.com", "second@example.com"} {
				_, err := db.Exec("INSERT INTO users (id, email, password) VALUES (?, ?, 'hash')", uuid.NewString(), email)
				require.NoError(t, err)
			}

			upgraded := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
			_, err = upgraded.Migrate(ctx)
			require.NoError(t, err)

			users := usersOf(t, db)
			require.Len(t, users, 2)
			for email, user := range users {
				if assert.NotNil(t, user.EmailVerifiedAt, email) {
					assert.WithinDuration(t, time.Now(), *user.EmailVerifiedAt, time.Minute, email)
				}
				assert.Nil(t, user.TokensValidAfter, "%s: no session was ended", email)
			}
			_, err = db.Exec("INSERT INTO users (id, email, password) VALUES (?, 'new@example.com', 'hash')", uuid.NewString())
			require.NoError(t, err)
			assert.Nil(t, usersOf(t, db)["new@example.com"].EmailVerifiedAt, "a user who signs up later is not verified")

			var tokens int
			require.NoError(t, db.NewSelect().Table("account_tokens").ColumnExpr("count(*)").Scan(ctx, &tokens), "the table exists")

			t.Run("a user's tokens go with the user", func(t *testing.T) {
				var id string
				require.NoError(t, db.NewSelect().Table("users").Column("id").Where("email = 'new@example.com'").Scan(ctx, &id))
				_, err := db.Exec("INSERT INTO account_tokens (id, user_id, purpose, token_hash, expires_at) VALUES (?, ?, 'reset', 'sha256$x', ?)", uuid.NewString(), id, time.Now().UTC().Add(time.Hour))
				require.NoError(t, err)
				_, err = db.Exec("DELETE FROM users WHERE id = ?", id)
				require.NoError(t, err)
				require.NoError(t, db.NewSelect().Table("account_tokens").ColumnExpr("count(*)").Scan(ctx, &tokens))
				assert.Zero(t, tokens)
			})

			t.Run("it rolls back", func(t *testing.T) {
				_, err := upgraded.Rollback(ctx)
				require.NoError(t, err)
				_, err = db.Exec("SELECT email_verified_at FROM users")
				assert.Error(t, err)
				_, err = db.Exec("SELECT 1 FROM account_tokens")
				assert.Error(t, err)
			})
		})
	}
}
