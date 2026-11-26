package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// A monitor has one TLS record, which a check replaces, and MongoDB's unique index on monitor_id
// enforces that. Where there is no index, a monitor can have several, and the import keeps the one
// that was updated last: a monitor's older record holds a certificate that has since been renewed.
func TestImportKeepsTheNewestTLSRecordOfAMonitor(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	monitorWithName := func(name string) primitive.ObjectID {
		return in.Insert(t, "monitor", bson.D{
			{Key: "type", Value: "http"}, {Key: "name", Value: name}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
			{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
			{Key: "status", Value: 1}, {Key: "config", Value: `{}`}, {Key: "push_token", Value: ""},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	record := func(monitor primitive.ObjectID, info string, updated time.Time) {
		document := bson.D{{Key: "monitor_id", Value: monitor.Hex()}, {Key: "info_json", Value: info}, {Key: "created_at", Value: now.Add(-48 * time.Hour)}}
		if !updated.IsZero() {
			document = append(document, bson.E{Key: "updated_at", Value: updated})
		}
		in.Insert(t, "monitor_tls_info", document)
	}

	// The record made first was updated last
	renewed := monitorWithName("Renewed")
	record(renewed, `{"valid":true,"daysRemaining":80}`, now.Add(-time.Hour))
	record(renewed, `{"valid":true,"daysRemaining":5}`, now.Add(-24*time.Hour))
	// Neither says when it was updated, so the one made last is the newest
	undated := monitorWithName("Undated")
	record(undated, `{"valid":true,"daysRemaining":3}`, time.Time{})
	record(undated, `{"valid":true,"daysRemaining":90}`, time.Time{})
	// A monitor with one record
	single := monitorWithName("Single")
	record(single, `{"valid":true,"daysRemaining":30}`, now)

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		tls := monitor_tls_info.NewSQLRepository(db)
		for monitor, want := range map[primitive.ObjectID]string{
			renewed: `{"valid":true,"daysRemaining":80}`,
			undated: `{"valid":true,"daysRemaining":90}`,
			single:  `{"valid":true,"daysRemaining":30}`,
		} {
			got, err := tls.GetByMonitorID(ctx, uuidOf(t, monitor.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, monitor.Hex())
			assert.JSONEq(t, want, got.InfoJSON)
		}

		r := collectionReport(t, report, "monitor_tls_info")
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 3, r.Copied)
		assert.Equal(t, map[string]int{"an older TLS record of the same monitor": 2}, r.Skipped)
	})
}
