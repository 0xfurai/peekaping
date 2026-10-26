package monitor

import (
	"context"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// addLocationRuleMigration is the migration that gave monitors their location rule
const addLocationRuleMigration = "20261026120000"

func TestSQLRepository_ExistingMonitorsReadAsAny(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addLocationRuleMigration {
					before.Add(m)
				}
			}
			migrateUp(t, db, before)
			id := uuid.NewString()
			_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
			require.NoError(t, err)

			migrateUp(t, db, migrations.Migrations)

			found, err := NewSQLRepository(db).FindByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, shared.LocationRuleAny, found.LocationRule)
			assert.Nil(t, found.LocationThreshold)
		})
	}
}

func TestSQLRepository_StoresTheLocationRule(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		addProbe(t, db, "paris")
		threshold := 2
		monitor := monitorOn("Checkout", true, "local", "frankfurt", "paris")
		monitor.LocationRule, monitor.LocationThreshold = shared.LocationRuleAtLeast, &threshold

		created, err := repo.Create(ctx, monitor)
		require.NoError(t, err)
		assert.Equal(t, shared.LocationRuleAtLeast, created.LocationRule, "the create answers with what is stored")
		require.NotNil(t, created.LocationThreshold)
		assert.Equal(t, 2, *created.LocationThreshold)
		stored := func() (shared.LocationRule, *int) {
			found, err := repo.FindByID(ctx, created.ID)
			require.NoError(t, err)
			return found.LocationRule, found.LocationThreshold
		}
		rule, got := stored()
		assert.Equal(t, shared.LocationRuleAtLeast, rule)
		assert.Equal(t, &threshold, got)

		renamed := *created
		renamed.Name = "Checkout, renamed"
		require.NoError(t, repo.UpdateFull(ctx, created.ID, &renamed))
		rule, got = stored()
		assert.Equal(t, shared.LocationRuleAtLeast, rule, "a full update writes the rule it is given")
		assert.Equal(t, &threshold, got)

		renamed.LocationRule, renamed.LocationThreshold = shared.LocationRuleAll, nil
		require.NoError(t, repo.UpdateFull(ctx, created.ID, &renamed))
		rule, got = stored()
		assert.Equal(t, shared.LocationRuleAll, rule)
		assert.Nil(t, got, "and the threshold goes with a rule that has none")

		name := "Checkout, again"
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{Name: &name}))
		rule, _ = stored()
		assert.Equal(t, shared.LocationRuleAll, rule, "a partial update without a rule keeps it")

		atLeast, three := shared.LocationRuleAtLeast, 3
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{LocationRule: &atLeast, LocationThreshold: &three}))
		rule, got = stored()
		assert.Equal(t, shared.LocationRuleAtLeast, rule)
		assert.Equal(t, &three, got)

		anyRule := shared.LocationRuleAny
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{LocationRule: &anyRule}))
		rule, got = stored()
		assert.Equal(t, shared.LocationRuleAny, rule)
		assert.Nil(t, got, "a partial update to a rule without a threshold clears it")
	})
}

func TestSQLRepository_AMonitorCreatedWithoutARuleIsAny(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		created, err := NewSQLRepository(db).Create(context.Background(), createTestMonitor("Checkout", true, shared.MonitorStatusPending))

		require.NoError(t, err)
		assert.Equal(t, shared.LocationRuleAny, created.LocationRule)
		assert.Nil(t, created.LocationThreshold)
	})
}
