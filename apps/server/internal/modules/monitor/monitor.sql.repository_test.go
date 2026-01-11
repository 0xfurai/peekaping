package monitor

import (
	"context"
	"database/sql"
	"testing"
	"time"
	"vigi/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

func setupTestDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:?cache=shared")
	require.NoError(t, err)

	db := bun.NewDB(sqldb, sqlitedialect.New())

	// Create monitors table
	_, err = db.Exec(`
		CREATE TABLE monitors (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			interval INTEGER NOT NULL,
			timeout INTEGER NOT NULL,
			max_retries INTEGER NOT NULL,
			retry_interval INTEGER NOT NULL,
			resend_interval INTEGER NOT NULL,
			active BOOLEAN NOT NULL DEFAULT TRUE,
			status INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			config TEXT,
			proxy_id TEXT,
			push_token TEXT,
			org_id TEXT
		)
	`)
	require.NoError(t, err)

	// Create monitor_tags table for testing JOIN functionality
	_, err = db.Exec(`
		CREATE TABLE monitor_tags (
			monitor_id TEXT NOT NULL,
			tag_id TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (monitor_id, tag_id)
		)
	`)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func createTestMonitor(name string, active bool, status shared.MonitorStatus) *shared.Monitor {
	return &shared.Monitor{
		Type:           "http",
		Name:           name,
		Interval:       60,
		Timeout:        30,
		MaxRetries:     3,
		RetryInterval:  60,
		ResendInterval: 5,
		Active:         active,
		Status:         status,
		Config:         `{"url": "https://example.com"}`,
		ProxyId:        "",
		PushToken:      "test-token",
	}
}

func TestSQLRepositoryImpl_FindAll_JoinAmbiguityFix(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	ctx := context.Background()

	// Create a monitor
	monitor := createTestMonitor("Test Monitor", true, shared.MonitorStatus(0))
	created, err := repo.Create(ctx, monitor)
	require.NoError(t, err)

	// Add a tag to trigger the JOIN
	_, err = db.Exec("INSERT INTO monitor_tags (monitor_id, tag_id, created_at) VALUES (?, ?, ?)",
		created.ID, "test-tag", time.Now().Add(-1*time.Hour))
	require.NoError(t, err)

	t.Run("FindAll_WithTagsNoAmbiguousColumn", func(t *testing.T) {
		// This should not fail with "ambiguous column name: created_at" error
		monitors, err := repo.FindAll(ctx, 0, 10, "", nil, nil, []string{"test-tag"}, "")

		require.NoError(t, err)
		assert.Len(t, monitors, 1)
		assert.Equal(t, created.ID, monitors[0].ID)

		// Verify the monitor's created_at is from the monitors table, not monitor_tags
		assert.WithinDuration(t, created.CreatedAt, monitors[0].CreatedAt, time.Second)
	})

	t.Run("FindAll_OrderingStillWorksWithJoin", func(t *testing.T) {
		// Create another monitor with tag to test ordering with JOIN
		monitor2 := createTestMonitor("Test Monitor 2", true, shared.MonitorStatus(0))
		created2, err := repo.Create(ctx, monitor2)
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO monitor_tags (monitor_id, tag_id, created_at) VALUES (?, ?, ?)",
			created2.ID, "test-tag", time.Now().Add(-2*time.Hour))
		require.NoError(t, err)

		monitors, err := repo.FindAll(ctx, 0, 10, "", nil, nil, []string{"test-tag"}, "")

		require.NoError(t, err)
		assert.Len(t, monitors, 2)

		// Should be ordered by monitors.created_at DESC (newest first)
		assert.Equal(t, created2.ID, monitors[0].ID)
		assert.Equal(t, created.ID, monitors[1].ID)
	})
}
