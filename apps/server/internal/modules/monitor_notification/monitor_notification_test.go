package monitor_notification_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor_notification"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "monitor_notification.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "monitor_notification_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return db
}

// fixture is a monitor-notification service plus constructors for the rows a link points at.
type fixture struct {
	svc     monitor_notification.Service
	monitor func() string
	channel func() string
}

// forEachBackend runs fn against a monitor-notification service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			fn(t, fixture{
				svc: monitor_notification.NewService(monitor_notification.NewSQLRepository(db), zap.NewNop().Sugar()),
				monitor: func() string {
					id := uuid.NewString()
					_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
					require.NoError(t, err)
					return id
				},
				channel: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO notification_channels (id, name, type) VALUES (?, 'Ops', 'webhook')", id)
					require.NoError(t, err)
					return id
				},
			})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
	t.Run("Mongo", func(t *testing.T) {
		uri := os.Getenv("PEE_TEST_MONGO_URI")
		if uri == "" {
			t.Skip("set PEE_TEST_MONGO_URI to run the MongoDB tests")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		require.NoError(t, err)
		if err := client.Ping(ctx, nil); err != nil {
			_ = client.Disconnect(context.Background())
			require.NoError(t, err)
		}
		cfg := &config.Config{DBName: "monitor_notification_" + primitive.NewObjectID().Hex()}
		t.Cleanup(func() {
			require.NoError(t, client.Database(cfg.DBName).Drop(context.Background()))
			require.NoError(t, client.Disconnect(context.Background()))
		})
		newID := func() string { return primitive.NewObjectID().Hex() }
		fn(t, fixture{monitor_notification.NewService(monitor_notification.NewMongoRepository(client, cfg), zap.NewNop().Sugar()), newID, newID})
	})
}

func link(t *testing.T, svc monitor_notification.Service, monitorID, channelID string) *monitor_notification.Model {
	t.Helper()
	created, err := svc.Create(context.Background(), monitorID, channelID)
	require.NoError(t, err)
	return created
}

func channelsOf(t *testing.T, svc monitor_notification.Service, monitorID string) []string {
	t.Helper()
	links, err := svc.FindByMonitorID(context.Background(), monitorID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.NotificationID)
	}
	return ids
}

func TestMonitorNotifications_LinksAreListedPerMonitor(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		web, db := f.monitor(), f.monitor()
		slack, email := f.channel(), f.channel()
		created := link(t, f.svc, web, slack)
		link(t, f.svc, web, email)
		link(t, f.svc, db, email)

		found, err := f.svc.FindByID(context.Background(), created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, web, found.MonitorID)
		assert.Equal(t, slack, found.NotificationID)

		assert.ElementsMatch(t, []string{slack, email}, channelsOf(t, f.svc, web))
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, db))
	})
}

// channelsByMonitor lists the channel IDs of each monitor's links, in the order they came.
func channelsByMonitor(links map[string][]*monitor_notification.Model) map[string][]string {
	byMonitor := map[string][]string{}
	for monitorID, monitorLinks := range links {
		for _, l := range monitorLinks {
			byMonitor[monitorID] = append(byMonitor[monitorID], l.NotificationID)
		}
	}
	return byMonitor
}

func TestMonitorNotifications_LinksOfManyMonitorsAreListedAtOnce(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		web, db, idle, other := f.monitor(), f.monitor(), f.monitor(), f.monitor()
		slack, email := f.channel(), f.channel()
		link(t, f.svc, web, slack)
		link(t, f.svc, web, email)
		link(t, f.svc, db, email)
		link(t, f.svc, other, slack)

		found, err := f.svc.FindByMonitorIDs(ctx, []string{web, db, idle})
		require.NoError(t, err)

		byMonitor := channelsByMonitor(found)
		assert.Len(t, byMonitor, 2, "the monitor with no links and the monitor not asked for have no entry")
		assert.ElementsMatch(t, []string{slack, email}, byMonitor[web])
		assert.Equal(t, []string{email}, byMonitor[db])
		assert.Equal(t, channelsOf(t, f.svc, web), byMonitor[web], "each monitor's links come in the order FindByMonitorID lists them")

		for _, l := range found[web] {
			assert.Equal(t, web, l.MonitorID)
		}

		none, err := f.svc.FindByMonitorIDs(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, none, "no monitors asked for, no links")
	})
}

func TestMonitorNotifications_DeletesRemoveOnlyTheLinksTheyName(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		web, db, api := f.monitor(), f.monitor(), f.monitor()
		slack, email := f.channel(), f.channel()
		webSlack := link(t, f.svc, web, slack)
		link(t, f.svc, web, email)
		link(t, f.svc, db, slack)
		link(t, f.svc, db, email)
		link(t, f.svc, api, slack)
		link(t, f.svc, api, email)

		require.NoError(t, f.svc.Delete(ctx, webSlack.ID))
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, web), "Delete removes that one link")
		gone, err := f.svc.FindByID(ctx, webSlack.ID)
		require.NoError(t, err)
		assert.Nil(t, gone)

		require.NoError(t, f.svc.DeleteByMonitorID(ctx, api))
		assert.Empty(t, channelsOf(t, f.svc, api))
		assert.ElementsMatch(t, []string{slack, email}, channelsOf(t, f.svc, db), "DeleteByMonitorID keeps other monitors' links")

		require.NoError(t, f.svc.DeleteByNotificationID(ctx, email))
		assert.Empty(t, channelsOf(t, f.svc, web))
		assert.ElementsMatch(t, []string{slack}, channelsOf(t, f.svc, db), "DeleteByNotificationID keeps other channels' links")
	})
}
