package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
)

// PostgreSQL rejects the escape \u0000 in a JSONB value, though it is valid JSON, and SQLite accepts
// it. Both databases get the config without the escape, so they hold the same data.
func TestImportRemovesNULEscapesFromConfigs(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// The second \u0000 is an escaped backslash followed by u0000, which is text and stays
	const config = `{"url":"https://example.test/a\u0000b","note":"keeps \\u0000 as text"}`
	const want = `{"url":"https://example.test/ab","note":"keeps \\u0000 as text"}`
	monitorID := in.Insert(t, "monitor", bson.D{
		{Key: "type", Value: "http"}, {Key: "name", Value: "NUL in the config"}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
		{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
		{Key: "status", Value: 1}, {Key: "config", Value: config}, {Key: "push_token", Value: ""},
		{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
	})
	channelID := in.Insert(t, "notification_channel", bson.D{
		{Key: "name", Value: "NUL in the config"}, {Key: "type", Value: "webhook"}, {Key: "active", Value: true}, {Key: "is_default", Value: false},
		{Key: "config", Value: config}, {Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
	})

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		got, err := monitor.NewSQLRepository(db).FindByID(ctx, importedWorkspace(t, db), uuidOf(t, monitorID.Hex()))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.JSONEq(t, want, got.Config)
		channel, err := notification_channel.NewSQLRepository(db).FindByID(ctx, uuidOf(t, channelID.Hex()))
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.JSONEq(t, want, *channel.Config)

		const fixed = `config had \u0000 escapes, which PostgreSQL rejects: removed`
		assert.Equal(t, map[string]int{fixed: 1}, collectionReport(t, report, "monitor").Fixed)
		assert.Equal(t, map[string]int{fixed: 1}, collectionReport(t, report, "notification_channel").Fixed)
	})
}
