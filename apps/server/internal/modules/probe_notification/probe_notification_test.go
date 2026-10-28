package probe_notification_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "probe_notification.db")}, zap.NewNop().Sugar())
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
	schema := "probe_notification_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// fixture is a probe-notification service plus constructors for the rows a link points at.
type fixture struct {
	db      *bun.DB
	svc     probe_notification.Service
	probe   func() string
	channel func() string
	// members are the workspaces the caller is a member of: the install's one workspace
	members []workspace.ID
	// channelIn makes a channel in the workspace, which it adds
	channelIn func(id workspace.ID) string
	// delete removes a channel the way the database does, to see its links go with it
	deleteChannel func(id string)
}

// forEachBackend runs fn against a probe-notification service on every SQL storage backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			first := workspace.ID(workspacetest.OldestOrAdd(t, db))
			fn(t, fixture{
				db:      db,
				members: []workspace.ID{first},
				channelIn: func(id workspace.ID) string {
					var exists int
					require.NoError(t, db.NewSelect().Table("workspaces").ColumnExpr("COUNT(*)").Where("id = ?", string(id)).Scan(ctx, &exists))
					if exists == 0 {
						workspacetest.Add(t, db, id, string(id))
					}
					channel := uuid.NewString()
					_, err := db.Exec("INSERT INTO notification_channels (id, workspace_id, name, type) VALUES (?, ?, 'Ops', 'webhook')", channel, string(id))
					require.NoError(t, err)
					return channel
				},
				svc: probe_notification.NewService(probe_notification.NewSQLRepository(db), zap.NewNop().Sugar()),
				probe: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO probes (id, name) VALUES (?, 'Probe')", id)
					require.NoError(t, err)
					return id
				},
				channel: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO notification_channels (id, workspace_id, name, type) VALUES (?, ?, 'Ops', 'webhook')", id, string(first))
					require.NoError(t, err)
					return id
				},
				deleteChannel: func(id string) {
					_, err := db.Exec("DELETE FROM notification_channels WHERE id = ?", id)
					require.NoError(t, err)
				},
			})
		})
	}
}

func channelsOf(t *testing.T, svc probe_notification.Service, probeID string) []string {
	t.Helper()
	links, err := svc.FindByProbeID(context.Background(), probeID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		assert.Equal(t, probeID, l.ProbeID)
		ids = append(ids, l.NotificationID)
	}
	return ids
}

func TestProbeNotifications_ReplaceSetsTheProbesChannels(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge, core := f.probe(), f.probe()
		slack, email, pager := f.channel(), f.channel(), f.channel()

		require.NoError(t, f.svc.Replace(ctx, edge, []string{slack, email}, f.members))
		require.NoError(t, f.svc.Replace(ctx, core, []string{email}, f.members))
		assert.ElementsMatch(t, []string{slack, email}, channelsOf(t, f.svc, edge))
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, core))

		require.NoError(t, f.svc.Replace(ctx, edge, []string{email, pager}, f.members))
		assert.ElementsMatch(t, []string{email, pager}, channelsOf(t, f.svc, edge), "Replace drops the channels it doesn't name")
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, core), "Replace keeps other probes' links")

		require.NoError(t, f.svc.Replace(ctx, edge, []string{}, f.members))
		assert.Empty(t, channelsOf(t, f.svc, edge))
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, core))
	})
}

func TestProbeNotifications_ReplaceCountsARepeatedChannelOnce(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		edge, slack := f.probe(), f.channel()

		require.NoError(t, f.svc.Replace(context.Background(), edge, []string{slack, slack, strings.ToUpper(slack)}, f.members))

		assert.Equal(t, []string{slack}, channelsOf(t, f.svc, edge))
	})
}

func TestProbeNotifications_ReplaceRefusesAnUnknownChannelAndChangesNothing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge := f.probe()
		slack, email := f.channel(), f.channel()
		require.NoError(t, f.svc.Replace(ctx, edge, []string{slack}, f.members))

		for name, unknown := range map[string]string{"no such channel": uuid.NewString(), "not an id": "nope"} {
			err := f.svc.Replace(ctx, edge, []string{email, unknown}, f.members)

			require.ErrorIs(t, err, probe_notification.ErrUnknownChannel, name)
			assert.ErrorContains(t, err, unknown, name)
			assert.Equal(t, []string{slack}, channelsOf(t, f.svc, edge), name)
		}
	})
}

// Replace and DeleteByNotificationID run on the transaction their ctx carries. The database has one
// connection here, as SQLite does in the app, so a call that opened its own transaction, or ran
// on db, would wait for the connection the caller's transaction holds, and fail when ctx times out.
func TestProbeNotifications_ReplaceAndDeleteJoinTheCallersTransaction(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		f.db.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		edge := f.probe()
		slack, email := f.channel(), f.channel()
		require.NoError(t, f.svc.Replace(ctx, edge, []string{slack}, f.members))
		transactor := dbtx.NewSQLTransactor(f.db)
		errRollBack := errors.New("roll back")

		err := transactor.WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, f.svc.Replace(ctx, edge, []string{email}, f.members))
			links, err := f.svc.FindByProbeID(ctx, edge)
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, email, links[0].NotificationID, "the transaction reads its own writes")
			require.NoError(t, f.svc.DeleteByNotificationID(ctx, email))
			return errRollBack
		})

		require.ErrorIs(t, err, errRollBack)
		assert.Equal(t, []string{slack}, channelsOf(t, f.svc, edge), "the swap and the delete were rolled back")

		err = transactor.WithTx(ctx, func(ctx context.Context) error {
			return f.svc.Replace(ctx, edge, []string{email}, f.members)
		})

		require.NoError(t, err)
		assert.Equal(t, []string{email}, channelsOf(t, f.svc, edge), "and a transaction that returns nil keeps its swap")
	})
}

func TestProbeNotifications_DeletingAChannelClearsItsLinks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge, core := f.probe(), f.probe()
		slack, email := f.channel(), f.channel()
		require.NoError(t, f.svc.Replace(ctx, edge, []string{slack, email}, f.members))
		require.NoError(t, f.svc.Replace(ctx, core, []string{slack}, f.members))

		require.NoError(t, f.svc.DeleteByNotificationID(ctx, slack))
		assert.ElementsMatch(t, []string{email}, channelsOf(t, f.svc, edge))
		assert.Empty(t, channelsOf(t, f.svc, core))

		f.deleteChannel(email)
		assert.Empty(t, channelsOf(t, f.svc, edge), "the database drops the links of a channel that is deleted")
	})
}

// An instance admin links a probe to the channels of a workspace they are a member of and to no
// other (docs/adr/0005 section 10): a channel of another workspace is refused as one that doesn't exist
func TestProbeNotifications_ReplaceRefusesAChannelOfAWorkspaceTheCallerIsNotIn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge := f.probe()
		mine, theirs := f.channel(), f.channelIn(workspacetest.Other)
		require.NoError(t, f.svc.Replace(ctx, edge, []string{mine}, f.members))

		err := f.svc.Replace(ctx, edge, []string{mine, theirs}, f.members)

		require.ErrorIs(t, err, probe_notification.ErrUnknownChannel)
		assert.ErrorContains(t, err, theirs, "it says what a channel that doesn't exist says")
		assert.Equal(t, []string{mine}, channelsOf(t, f.svc, edge), "and changes nothing")
		unknown := f.svc.Replace(ctx, edge, []string{mine, uuid.NewString()}, f.members)
		require.ErrorIs(t, unknown, probe_notification.ErrUnknownChannel)
	})
}

func TestProbeNotifications_ReplaceTakesChannelsOfEveryWorkspaceTheCallerIsIn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge := f.probe()
		mine, theirs := f.channel(), f.channelIn(workspacetest.Other)

		require.NoError(t, f.svc.Replace(ctx, edge, []string{mine, theirs}, append(f.members, workspacetest.Other)))

		assert.ElementsMatch(t, []string{mine, theirs}, channelsOf(t, f.svc, edge))
	})
}

func TestProbeNotifications_ACallerInNoWorkspaceLinksNoChannel(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge, channel := f.probe(), f.channel()

		err := f.svc.Replace(ctx, edge, []string{channel}, nil)

		require.ErrorIs(t, err, probe_notification.ErrUnknownChannel)
		assert.Empty(t, channelsOf(t, f.svc, edge))
		require.NoError(t, f.svc.Replace(ctx, edge, []string{}, nil), "a list with no channel clears the probe's")
	})
}
