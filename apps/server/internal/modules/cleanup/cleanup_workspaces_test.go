package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

func sqliteInstall(t *testing.T) install {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "cleanup.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return newInstallOn(t, db)
}

// postgresInstall is an install in an empty schema of the database at PEE_TEST_POSTGRES_DSN
func postgresInstall(t *testing.T) install {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "cleanup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	return newInstallOn(t, db)
}

// monitorIn adds a monitor to the workspace and returns its ID
func (in install) monitorIn(t *testing.T, id workspace.ID) string {
	t.Helper()
	monitorID := uuid.NewString()
	_, err := in.db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID, string(id))
	require.NoError(t, err)
	return monitorID
}

// heartbeatsOf records a heartbeat of the monitor for each age in days, with the age as its message
func (in install) heartbeatsOf(t *testing.T, monitorID string, ages ...int) {
	t.Helper()
	now := time.Now().UTC()
	for _, days := range ages {
		row := &heartbeatRow{ID: uuid.NewString(), MonitorID: monitorID, Status: 1, Msg: fmt.Sprintf("%dd", days), Time: now.AddDate(0, 0, -days)}
		_, err := in.db.NewInsert().Model(row).Exec(context.Background())
		require.NoError(t, err)
	}
}

func (in install) heartbeatsOfLeft(t *testing.T, monitorID string) []string {
	t.Helper()
	left, err := in.heartbeats.FindByMonitorIDPaginated(context.Background(), monitorID, 100, 0, nil, true)
	require.NoError(t, err)
	msgs := []string{}
	for _, hb := range left {
		msgs = append(msgs, hb.Msg)
	}
	return msgs
}

// AC4: each workspace keeps the history its own period asks for
func TestCleanupHeartbeatsKeepsEachWorkspacesOwnHistory(t *testing.T) {
	for name, open := range map[string]func(*testing.T) install{"SQLite": sqliteInstall, "Postgres": postgresInstall} {
		t.Run(name, func(t *testing.T) {
			in := open(t)
			oneDay := in.workspaceID(t)
			tenDays := workspace.ID(uuid.NewString())
			workspacetest.Add(t, in.db, tenDays, "Ten days")
			in.storePeriod(t, oneDay, "1")
			in.storePeriod(t, tenDays, "10")
			shortMonitor := in.monitorID
			longMonitor := in.monitorIn(t, tenDays)
			in.heartbeatsOf(t, shortMonitor, 20, 5, 0)
			in.heartbeatsOf(t, longMonitor, 20, 5, 0)

			cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, zap.NewNop().Sugar())

			assert.Equal(t, []string{"0d"}, in.heartbeatsOfLeft(t, shortMonitor), "the workspace with one day keeps today's")
			assert.Equal(t, []string{"5d", "0d"}, in.heartbeatsOfLeft(t, longMonitor), "the workspace with ten days keeps today's and the one from five days ago")
		})
	}
}

func TestCleanupHeartbeatsGoesOnWithTheOtherWorkspacesWhenOnePeriodIsUnusable(t *testing.T) {
	in := newInstall(t)
	broken := in.workspaceID(t)
	healthy := workspace.ID(uuid.NewString())
	workspacetest.Add(t, in.db, healthy, "Healthy")
	in.storePeriod(t, broken, "abc")
	in.storePeriod(t, healthy, "10")
	brokenMonitor := in.monitorID
	healthyMonitor := in.monitorIn(t, healthy)
	in.heartbeatsOf(t, brokenMonitor, 20, 0)
	in.heartbeatsOf(t, healthyMonitor, 20, 0)

	cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, zap.NewNop().Sugar())

	assert.Equal(t, []string{"20d", "0d"}, in.heartbeatsOfLeft(t, brokenMonitor))
	assert.Equal(t, []string{"0d"}, in.heartbeatsOfLeft(t, healthyMonitor))
}
