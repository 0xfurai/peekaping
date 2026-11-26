package auth_test

import (
	"context"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// failingStep is a bun query hook that fails the statement of one step of AccountRemover.Remove,
// by giving it a context that is cancelled already: the statement is refused before it reaches
// the database, as a failing step would be, and the code under test is not changed for it.
type failingStep struct {
	matches func(query string) bool
	armed   bool
	fired   bool
}

func (f *failingStep) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	if !f.armed || !f.matches(event.Query) {
		return ctx
	}
	f.fired = true
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	return cancelled
}

func (f *failingStep) AfterQuery(context.Context, *bun.QueryEvent) {}

func TestAccountRemover_AFailureAtAnyStepDeletesNothingAndAnnouncesNothing(t *testing.T) {
	steps := map[string]func(query string) bool{
		"the instance admins can't be locked": func(q string) bool {
			return strings.HasPrefix(q, "SELECT") && strings.Contains(q, `FROM "users"`) && strings.Contains(q, "ORDER BY")
		},
		"the user can't be read": func(q string) bool {
			return strings.HasPrefix(q, "SELECT") && strings.Contains(q, `FROM "users"`) && !strings.Contains(q, "ORDER BY")
		},
		"the workspaces owned alone can't be read": func(q string) bool { return strings.Contains(q, "workspace_members AS m") },
		"the workspaces of the user can't be read": func(q string) bool {
			return strings.HasPrefix(q, "SELECT") && strings.Contains(q, `"joined_at"`)
		},
		"the user can't be deleted": func(q string) bool { return strings.HasPrefix(q, `DELETE FROM "users"`) },
	}
	for name, matches := range steps {
		t.Run(name, func(t *testing.T) {
			for backend, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
				t.Run(backend, func(t *testing.T) {
					db := open(t)
					migrator := migrate.NewMigrator(db, migrations.Migrations)
					require.NoError(t, migrator.Init(context.Background()))
					_, err := migrator.Migrate(context.Background())
					require.NoError(t, err)
					step := &failingStep{matches: matches}
					db.AddQueryHook(step)
					bus := eventstest.NewBus()
					remover := auth.NewAccountRemover(db, dbtx.NewSQLTransactor(db), bus, zap.NewNop().Sugar())
					keeper, target := uuid.NewString(), uuid.NewString()
					for id, admin := range map[string]bool{keeper: true, target: false} {
						_, err := db.Exec("INSERT INTO users (id, email, password, instance_admin, active) VALUES (?, ?, 'hash', ?, true)", id, id+"@example.com", admin)
						require.NoError(t, err)
					}
					shared, err := workspace.NewSQLRepository(db).Create(context.Background(), "Shared")
					require.NoError(t, err)
					require.NoError(t, workspace.NewSQLRepository(db).AddMember(context.Background(), shared.ID, keeper, workspace.RoleOwner))
					require.NoError(t, workspace.NewSQLRepository(db).AddMember(context.Background(), shared.ID, target, workspace.RoleMember))
					step.armed = true

					err = remover.Remove(context.Background(), target)

					require.ErrorIs(t, err, context.Canceled)
					assert.True(t, step.fired, "the step ran")
					assert.Empty(t, bus.Topics())
					step.armed = false
					var users, memberships int
					require.NoError(t, db.NewSelect().Table("users").ColumnExpr("COUNT(*)").Where("id = ?", target).Scan(context.Background(), &users))
					require.NoError(t, db.NewSelect().Table("workspace_members").ColumnExpr("COUNT(*)").Where("user_id = ?", target).Scan(context.Background(), &memberships))
					assert.Equal(t, 1, users, "the user is still there")
					assert.Equal(t, 1, memberships, "and keeps the place in the workspace")

					require.NoError(t, remover.Remove(context.Background(), target), "and the same removal goes through without the failure")
					assert.Equal(t, []string{"auth.sessions.ended", "workspace.member.left"}, bus.Topics())
				})
			}
		})
	}
}
