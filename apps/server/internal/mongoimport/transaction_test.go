package mongoimport_test

import (
	"context"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/migrate"
)

var peekapingTables = []string{
	"users", "settings", "api_keys",
	"proxies", "monitors", "notification_channels", "tags", "maintenances", "status_pages",
	"monitor_notifications", "monitor_tags", "monitor_maintenances", "monitor_status_pages", "domain_status_page",
	"monitor_tls_info", "notification_sent_history",
	"heartbeats", "stat_minutely", "stat_hourly", "stat_daily",
}

// The heartbeats are copied after every table but the stats. If they fail, the tables copied
// before them must not keep their rows.
func TestImportLeavesTheTargetEmptyWhenItFails(t *testing.T) {
	in := mongoimporttest.Seed(t)
	ctx := context.Background()

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		require.NoError(t, migrator.Init(ctx))
		_, err := migrator.Migrate(ctx)
		require.NoError(t, err)
		if db.Dialect().Name() == dialect.SQLite {
			_, err = db.Exec(`CREATE TRIGGER no_heartbeats BEFORE INSERT ON heartbeats BEGIN SELECT RAISE(ABORT, 'no heartbeats'); END`)
		} else {
			_, err = db.Exec(`ALTER TABLE heartbeats ADD CONSTRAINT no_heartbeats CHECK (false)`)
		}
		require.NoError(t, err)

		_, err = mongoimport.Run(ctx, db, mongoimport.Options{MongoURI: in.URI, MongoDatabase: in.Database})
		assert.ErrorContains(t, err, "write heartbeats")
		for table, rows := range tableCounts(t, db, peekapingTables...) {
			assert.Zero(t, rows, table)
		}
	})
}
