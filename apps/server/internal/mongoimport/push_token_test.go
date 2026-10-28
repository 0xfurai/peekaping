package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Push tokens are unique in SQL (PP-146). MongoDB gave a monitor without a token an empty one,
// and let two monitors hold the same one. The import stores NULL for no token, and the first of
// two monitors with a token keeps it.
func TestImportStoresNoPushTokenAsNullAndKeepsASharedTokenOnTheFirstMonitor(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	insert := func(name, token string) primitive.ObjectID {
		return in.Insert(t, "monitor", bson.D{
			{Key: "type", Value: "push"}, {Key: "name", Value: name}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
			{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
			{Key: "status", Value: 1}, {Key: "config", Value: `{}`}, {Key: "push_token", Value: token},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	first, second := insert("First", "shared-token"), insert("Second", "shared-token")
	own := insert("Own", "own-token")
	noneA, noneB := insert("None A", ""), insert("None B", "")

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		monitors := monitor.NewSQLRepository(db)
		for id, want := range map[primitive.ObjectID]string{first: "shared-token", second: "", own: "own-token", noneA: "", noneB: ""} {
			got, err := monitors.FindByID(ctx, uuidOf(t, id.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, want, got.PushToken, got.Name)
		}
		var stored []*string
		require.NoError(t, db.NewSelect().Table("monitors").Column("push_token").Where("push_token IS NULL").Scan(ctx, &stored))
		assert.Len(t, stored, 3, "an empty token is NULL, not an empty string")

		r := collectionReport(t, report, "monitor")
		assert.Equal(t, 5, r.Copied)
		assert.Equal(t, 1, r.Fixed["push_token is held by an earlier monitor: cleared"])
	})
}
