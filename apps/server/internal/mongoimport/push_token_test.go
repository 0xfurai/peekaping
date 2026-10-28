package mongoimport_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
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

// A push monitor that was saved through the API once held its token in its config only. The
// upgrade fills push_token from the config for it (migration 20261101120300), and the import does
// the same, so that its push URL keeps working.
func TestImportFillsAnEmptyPushTokenOfAPushMonitorFromItsConfig(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	insert := func(name, typ, token, config string) primitive.ObjectID {
		return in.Insert(t, "monitor", bson.D{
			{Key: "type", Value: typ}, {Key: "name", Value: name}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
			{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
			{Key: "status", Value: 1}, {Key: "config", Value: config}, {Key: "push_token", Value: token},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	own := insert("Own", "push", "own-token", `{"pushToken":"other-token"}`)
	fromConfig := insert("From config", "push", "", `{"pushToken":"config-token"}`)
	emptyEverywhere := insert("Empty everywhere", "push", "", `{"pushToken":""}`)
	notPush := insert("Not push", "http", "", `{"pushToken":"ignored-token"}`)
	brokenConfig := insert("Odd config", "push", "", `{"pushToken":5}`)
	noToken := insert("No token", "push", "", `{}`)
	// The config token is the one an earlier monitor holds, so this one goes without it
	twin := insert("Twin", "push", "", `{"pushToken":"own-token"}`)

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		monitors := monitor.NewSQLRepository(db)
		for id, want := range map[primitive.ObjectID]string{
			own: "own-token", fromConfig: "config-token", emptyEverywhere: "", notPush: "", brokenConfig: "", noToken: "", twin: "",
		} {
			got, err := monitors.FindByID(ctx, uuidOf(t, id.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, want, got.PushToken, got.Name)
		}
		var withToken []string
		require.NoError(t, db.NewSelect().Table("monitors").Column("push_token").Where("push_token IS NOT NULL").Order("push_token").Scan(ctx, &withToken))
		assert.Equal(t, []string{"config-token", "own-token"}, withToken, "the others are NULL, not empty strings")

		r := collectionReport(t, report, "monitor")
		assert.Equal(t, 2, r.Fixed["push_token is empty: used the one in the config"], "From config, and Twin before it is cleared")
		assert.Equal(t, 1, r.Fixed["push_token is held by an earlier monitor: cleared"])
	})
}

// PostgreSQL cuts a push token to the 255 characters of its column, and SQLite keeps it whole. Two
// tokens that differ only after the 255th character are one token on PostgreSQL, which the unique
// index rejects, so the import checks for a duplicate on the cut token: the second monitor goes
// without, as it does for any other duplicate, and the import doesn't roll back.
func TestImportTreatsPushTokensThatAreTheSameOnceCutAsDuplicates(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	long := strings.Repeat("t", 255)
	insert := func(name, token string) primitive.ObjectID {
		return in.Insert(t, "monitor", bson.D{
			{Key: "type", Value: "push"}, {Key: "name", Value: name}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
			{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
			{Key: "status", Value: 1}, {Key: "config", Value: `{}`}, {Key: "push_token", Value: token},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	first, second := insert("First", long+"1"), insert("Second", long+"2")

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		monitors := monitor.NewSQLRepository(db)
		r := collectionReport(t, report, "monitor")
		assert.Equal(t, 2, r.Copied)
		got, err := monitors.FindByID(ctx, uuidOf(t, first.Hex()))
		require.NoError(t, err)
		require.NotNil(t, got)
		other, err := monitors.FindByID(ctx, uuidOf(t, second.Hex()))
		require.NoError(t, err)
		require.NotNil(t, other)
		if db.Dialect().Name() == dialect.PG {
			assert.Equal(t, long, got.PushToken)
			assert.Equal(t, "", other.PushToken)
			assert.Equal(t, 1, r.Fixed["push_token is held by an earlier monitor: cleared"])
			assert.Equal(t, 1, r.Fixed["push_token cut to 255 characters"], "the first; the second is cleared")
		} else {
			assert.Equal(t, long+"1", got.PushToken)
			assert.Equal(t, long+"2", other.PushToken)
			assert.Empty(t, r.Fixed)
		}
	})
}
