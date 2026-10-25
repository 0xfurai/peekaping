package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// rawDocument is a document of the install as MongoDB stores it, stray keys included.
func rawDocument(t *testing.T, in *mongoimporttest.Install, collection, objectID string) bson.M {
	t.Helper()
	var raw bson.M
	id, err := primitive.ObjectIDFromHex(objectID)
	require.NoError(t, err)
	require.NoError(t, in.Collection(collection).FindOne(context.Background(), bson.M{"_id": id}).Decode(&raw))
	return raw
}

// uuids maps ObjectIDs to the UUIDs they become.
func uuids(t *testing.T, objectIDs ...string) []string {
	t.Helper()
	out := make([]string, len(objectIDs))
	for i, id := range objectIDs {
		out[i] = uuidOf(t, id)
	}
	return out
}

func checkConfiguration(t *testing.T, in *mongoimporttest.Install, db *bun.DB, report *mongoimport.Report) {
	ctx := context.Background()

	t.Run("proxies", func(t *testing.T) {
		sqlProxies := proxy.NewSQLRepository(db)
		for _, name := range []string{"auth", "open"} {
			want := mongoimporttest.GetByID[mongoimporttest.ProxyDocument](t, in, "proxies", in.ID("proxy", name))
			got, err := sqlProxies.FindByID(ctx, uuidOf(t, want.ID.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, name)
			assert.Equal(t, want.Protocol, got.Protocol)
			assert.Equal(t, want.Host, got.Host)
			assert.Equal(t, want.Port, got.Port)
			assert.Equal(t, want.Auth, got.Auth)
			assert.Equal(t, want.Username, got.Username)
			assert.Equal(t, want.Password, got.Password)
			assertSameTime(t, want.CreatedAt, got.CreatedAt)
			assertSameTime(t, want.UpdatedAt, got.UpdatedAt)
		}
		r := collectionReport(t, report, "proxies")
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 2, r.Copied)
	})

	t.Run("monitors", func(t *testing.T) {
		sqlMonitors := monitor.NewSQLRepository(db)
		for _, name := range []string{"checkout", "nightly", "paused", "broken", "wide"} {
			want := mongoimporttest.GetByID[mongoimporttest.MonitorDocument](t, in, "monitor", in.ID("monitor", name))
			got, err := sqlMonitors.FindByID(ctx, uuidOf(t, want.ID.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, name)

			// What a monitor needs to run is copied as it is, apart from what the report lists
			wantName, wantConfig, wantProxy := want.Name, want.Config, ""
			if want.ProxyID != nil {
				wantProxy = uuidOf(t, want.ProxyID.Hex())
			}
			switch name {
			case "wide":
				wantName, wantProxy = cut(db, mongoimporttest.LongName, 150), "" // the proxy is gone
			case "paused", "broken":
				wantConfig = "{}"
			}
			assert.Equal(t, wantName, got.Name, name)
			assert.Equal(t, want.Type, got.Type, name)
			assert.Equal(t, want.Interval, got.Interval, name)
			assert.Equal(t, want.Timeout, got.Timeout, name)
			assert.Equal(t, want.MaxRetries, got.MaxRetries, name)
			assert.Equal(t, want.RetryInterval, got.RetryInterval, name)
			assert.Equal(t, want.ResendInterval, got.ResendInterval, name)
			assert.Equal(t, want.Active, got.Active, name)
			assert.Equal(t, want.Status, int(got.Status), name)
			assert.Equal(t, want.PushToken, got.PushToken, name)
			assert.Equal(t, wantProxy, got.ProxyId, name)
			assert.JSONEq(t, wantConfig, got.Config, name)
			assertSameTime(t, want.CreatedAt, got.CreatedAt, name)
			assertSameTime(t, want.UpdatedAt, got.UpdatedAt, name)
		}
		checkout, err := sqlMonitors.FindByID(ctx, uuidOf(t, in.ID("monitor", "checkout")))
		require.NoError(t, err)
		assert.Equal(t, uuidOf(t, in.ID("proxy", "auth")), checkout.ProxyId)
		nightly, err := sqlMonitors.FindOneByPushToken(ctx, "nightly-token")
		require.NoError(t, err)
		require.NotNil(t, nightly)
		assert.Equal(t, uuidOf(t, in.ID("monitor", "nightly")), nightly.ID)

		r := collectionReport(t, report, "monitor")
		assert.Equal(t, "monitors", r.Table)
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 5, r.Copied)
		assert.Zero(t, r.SkippedTotal())
		wantFixed := map[string]int{
			"proxy_id names a proxy that no longer exists: cleared": 1,
			"config is empty: used {}":                              1,
			"config is not valid JSON: used {}":                     1,
		}
		if db.Dialect().Name() == dialect.PG {
			// Only PostgreSQL limits the width of a name
			wantFixed["name cut to 150 characters"] = 1
		}
		assert.Equal(t, wantFixed, r.Fixed)
	})

	t.Run("notification channels", func(t *testing.T) {
		sqlChannels := notification_channel.NewSQLRepository(db)
		// The teams channel was saved in full and then changed in part, so the app showed the later change
		for _, name := range []string{"webhook", "email", "teams"} {
			want := mongoimporttest.GetByID[mongoimporttest.NotificationChannelDocument](t, in, "notification_channel", in.ID("channel", name))
			got, err := sqlChannels.FindByID(ctx, uuidOf(t, want.ID.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, name)
			assert.Equal(t, want.Name, got.Name)
			assert.Equal(t, want.Type, got.Type)
			assert.Equal(t, want.Active, got.Active)
			assert.Equal(t, want.IsDefault, got.IsDefault, name)
			require.NotNil(t, got.Config)
			assert.JSONEq(t, *want.Config, *got.Config)
			assertSameTime(t, want.CreatedAt, got.CreatedAt)
			assertSameTime(t, want.UpdatedAt, got.UpdatedAt)
		}

		// A full update wrote the new is_default and updated_at under keys of their own, and the
		// channel in the app showed the old ones. The update is the real state.
		slack, err := sqlChannels.FindByID(ctx, uuidOf(t, in.ID("channel", "slack")))
		require.NoError(t, err)
		require.NotNil(t, slack)
		raw := rawDocument(t, in, "notification_channel", in.ID("channel", "slack"))
		require.Contains(t, raw, "isdefault", "the seed doesn't have the stray keys of a full update")
		assert.Equal(t, "Slack ops", slack.Name)
		assert.True(t, slack.IsDefault)
		assert.JSONEq(t, `{"slack_webhook_url":"https://hooks.slack.example/b"}`, *slack.Config)
		assertSameTime(t, raw["created_at"].(primitive.DateTime).Time(), slack.CreatedAt)
		assertSameTime(t, raw["updatedat"].(primitive.DateTime).Time(), slack.UpdatedAt)
		assert.True(t, slack.UpdatedAt.After(slack.CreatedAt))

		// A channel without a config gets an empty one
		bare, err := sqlChannels.FindByID(ctx, uuidOf(t, in.ID("channel", "bare")))
		require.NoError(t, err)
		require.NotNil(t, bare.Config)
		assert.JSONEq(t, `{}`, *bare.Config)

		r := collectionReport(t, report, "notification_channel")
		assert.Equal(t, "notification_channels", r.Table)
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 5, r.Copied)
		assert.Equal(t, map[string]int{
			"is_default and updated_at taken from the keys of a full update": 1,
			"config is missing: used {}":                                     1,
		}, r.Fixed)
	})

	t.Run("tags", func(t *testing.T) {
		sqlTags := tag.NewSQLRepository(db)
		for _, name := range []string{"production", "staging"} {
			want := mongoimporttest.GetByID[mongoimporttest.TagDocument](t, in, "tags", in.ID("tag", name))
			got, err := sqlTags.FindByID(ctx, uuidOf(t, want.ID.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, name)
			assert.Equal(t, want.Name, got.Name)
			assert.Equal(t, want.Color, got.Color)
			assert.Equal(t, want.Description, got.Description)
			assertSameTime(t, want.CreatedAt, got.CreatedAt)
		}
		alpha, err := sqlTags.FindByName(ctx, "alpha")
		require.NoError(t, err)
		require.NotNil(t, alpha)
		assert.Equal(t, cut(db, "#3B82F6FF", 7), alpha.Color)

		r := collectionReport(t, report, "tags")
		assert.Equal(t, 3, r.Read)
		assert.Equal(t, 3, r.Copied)
		if db.Dialect().Name() == dialect.PG {
			assert.Equal(t, map[string]int{"color cut to 7 characters": 1}, r.Fixed)
		} else {
			assert.Empty(t, r.Fixed)
		}
	})

	t.Run("maintenance windows", func(t *testing.T) {
		sqlWindows := maintenance.NewSQLRepository(db)
		get := func(name string) *maintenance.Model {
			t.Helper()
			got, err := sqlWindows.FindByID(ctx, uuidOf(t, in.ID("maintenance", name)))
			require.NoError(t, err)
			require.NotNil(t, got, name)
			return got
		}

		// Created, then saved in full: that leaves created_at zero
		weekly := get("weekly")
		raw := rawDocument(t, in, "maintenance", in.ID("maintenance", "weekly"))
		assert.Equal(t, "Weekly patching", weekly.Title)
		assert.Equal(t, "Kernel updates", weekly.Description)
		assert.True(t, weekly.Active)
		assert.Equal(t, "recurring-weekday", weekly.Strategy)
		assert.Equal(t, "22:00", *weekly.StartTime)
		assert.Equal(t, "23:30", *weekly.EndTime)
		assert.Equal(t, []int{1, 3}, weekly.Weekdays)
		assert.Empty(t, weekly.DaysOfMonth)
		assert.Equal(t, "Europe/Kyiv", *weekly.Timezone)
		assert.True(t, weekly.CreatedAt.Equal(mustObjectID(t, in.ID("maintenance", "weekly")).Timestamp()), "%s", weekly.CreatedAt)
		assertSameTime(t, raw["updated_at"].(primitive.DateTime).Time(), weekly.UpdatedAt)

		// Switched off, which writes updated_at as an RFC 3339 string
		release := get("release")
		raw = rawDocument(t, in, "maintenance", in.ID("maintenance", "release"))
		updated, err := time.Parse(time.RFC3339, raw["updated_at"].(string))
		require.NoError(t, err)
		assert.False(t, release.Active)
		assert.Equal(t, "single", release.Strategy)
		assert.Equal(t, "2026-12-01T10:00", *release.StartDateTime)
		assert.Equal(t, "2026-12-01T12:00", *release.EndDateTime)
		assert.Equal(t, "UTC", *release.Timezone)
		assert.True(t, release.UpdatedAt.Equal(updated), "%s is not %s", release.UpdatedAt, updated)
		assertSameTime(t, raw["created_at"].(primitive.DateTime).Time(), release.CreatedAt)

		// Changed in part: the same, with the arrays and numbers the change set
		cron := get("cron")
		raw = rawDocument(t, in, "maintenance", in.ID("maintenance", "cron"))
		updated, err = time.Parse(time.RFC3339, raw["updated_at"].(string))
		require.NoError(t, err)
		assert.Equal(t, "cron", cron.Strategy)
		assert.Equal(t, "0 3 * * *", *cron.Cron)
		assert.Equal(t, 30, *cron.Duration)
		assert.Equal(t, 2, *cron.IntervalDay)
		assert.Equal(t, []int{1, 15}, cron.DaysOfMonth)
		assert.Empty(t, cron.Weekdays)
		assert.True(t, cron.UpdatedAt.Equal(updated), "%s is not %s", cron.UpdatedAt, updated)

		r := collectionReport(t, report, "maintenance")
		assert.Equal(t, "maintenances", r.Table)
		assert.Equal(t, 3, r.Read)
		assert.Equal(t, 3, r.Copied)
		assert.Equal(t, map[string]int{"created_at missing or zero: used the time the ObjectID was made": 1}, r.Fixed)
	})

	t.Run("status pages", func(t *testing.T) {
		pages, domains, links := status_page.NewSQLRepository(db), domain_status_page.NewSQLRepository(db), monitor_status_page.NewSQLRepository(db)
		shop, err := pages.FindBySlug(ctx, "shop")
		require.NoError(t, err)
		require.NotNil(t, shop)
		raw := rawDocument(t, in, "status_pages", in.ID("status_page", "shop"))
		assert.Equal(t, uuidOf(t, in.ID("status_page", "shop")), shop.ID)
		assert.Equal(t, "Shop status", shop.Title)
		assert.Equal(t, "Is the shop up?", shop.Description)
		assert.Equal(t, "https://example.com/icon.png", shop.Icon)
		assert.Equal(t, "dark", shop.Theme)
		assert.True(t, shop.Published)
		assert.Equal(t, "Thanks", shop.FooterText)
		assert.Equal(t, 60, shop.AutoRefreshInterval)
		assertSameTime(t, raw["created_at"].(primitive.DateTime).Time(), shop.CreatedAt)
		internal, err := pages.FindBySlug(ctx, "internal")
		require.NoError(t, err)
		require.NotNil(t, internal)
		assert.False(t, internal.Published)

		shopDomains, err := domains.GetDomainsForStatusPage(ctx, shop.ID)
		require.NoError(t, err)
		require.Len(t, shopDomains, 1)
		assert.Equal(t, "status.example.com", shopDomains[0].Domain)

		// Every monitor of a page comes across, hidden or not, and the page lists them in the order
		// the MongoDB page showed them, by the position it kept for each
		shopLinks, err := links.GetMonitorsForStatusPage(ctx, shop.ID)
		require.NoError(t, err)
		var linked []string
		for _, link := range shopLinks {
			linked = append(linked, link.MonitorID)
		}
		mongoLinks := mongoimporttest.List[mongoimporttest.MonitorStatusPageDocument](t, in, "monitor_status_page",
			bson.M{"status_page_id": mustObjectID(t, in.ID("status_page", "shop"))}, bson.D{{Key: "order", Value: 1}})
		var shown []string
		for _, link := range mongoLinks {
			shown = append(shown, link.MonitorID.Hex())
		}
		require.Equal(t, []string{in.ID("monitor", "nightly"), in.ID("monitor", "paused"), in.ID("monitor", "checkout")}, shown, "the order MongoDB showed")
		assert.Equal(t, uuids(t, shown...), linked)
		internalLinks, err := links.GetMonitorsForStatusPage(ctx, internal.ID)
		require.NoError(t, err)
		require.Len(t, internalLinks, 1)

		r := collectionReport(t, report, "status_pages")
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 2, r.Copied)
		r = collectionReport(t, report, "domain_status_page")
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 1, r.Copied)
		assert.Equal(t, map[string]int{"status page no longer exists": 1}, r.Skipped)
		r = collectionReport(t, report, "monitor_status_page")
		assert.Equal(t, "monitor_status_pages", r.Table)
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 4, r.Copied)
		assert.Equal(t, map[string]int{"status page no longer exists": 1}, r.Skipped)
		assert.Equal(t, map[string]int{
			"created_at missing or zero: used the time the ObjectID was made":  1, // the page with one monitor, saved in full
			"created_at set to keep the monitor's position on its status page": 3, // the three monitors of the shop page
		}, r.Fixed)

		// What SQL has no place for is reported
		assert.Equal(t, []string{
			`status page "shop": 1 monitor is hidden on the page (active is false) and will show (SQL has no column for it)`,
			`status page "shop": search_engine_index, password and google_analytics_tag_id are not kept (Peekaping does not use them)`,
		}, report.Notes)
	})

	t.Run("links", func(t *testing.T) {
		checkout, nightly := uuidOf(t, in.ID("monitor", "checkout")), uuidOf(t, in.ID("monitor", "nightly"))

		channels := monitor_notification.NewSQLRepository(db)
		for monitorID, want := range map[string][]string{
			checkout: uuids(t, in.ID("channel", "webhook"), in.ID("channel", "email")),
			nightly:  uuids(t, in.ID("channel", "email")),
		} {
			links, err := channels.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			var got []string
			for _, link := range links {
				got = append(got, link.NotificationID)
			}
			assert.ElementsMatch(t, want, got)
		}
		r := collectionReport(t, report, "monitor_notification")
		assert.Equal(t, "monitor_notifications", r.Table)
		assert.Equal(t, 4, r.Read)
		assert.Equal(t, 3, r.Copied)
		assert.Equal(t, map[string]int{"notification channel no longer exists": 1}, r.Skipped)

		tags := monitor_tag.NewSQLRepository(db)
		for monitorID, want := range map[string][]string{
			checkout: uuids(t, in.ID("tag", "production"), in.ID("tag", "staging")),
			nightly:  uuids(t, in.ID("tag", "production")),
		} {
			links, err := tags.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			var got []string
			for _, link := range links {
				got = append(got, link.TagID)
			}
			assert.ElementsMatch(t, want, got)
		}
		r = collectionReport(t, report, "monitor_tags")
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 3, r.Copied)
		assert.Equal(t, map[string]int{"monitor no longer exists": 1, "tag no longer exists": 1}, r.Skipped)

		windows := monitor_maintenance.NewSQLRepository(db)
		for monitorID, want := range map[string][]string{
			checkout: uuids(t, in.ID("maintenance", "weekly"), in.ID("maintenance", "release")),
			nightly:  uuids(t, in.ID("maintenance", "weekly")),
		} {
			links, err := windows.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			var got []string
			for _, link := range links {
				got = append(got, link.MaintenanceID)
			}
			assert.ElementsMatch(t, want, got)
		}
		r = collectionReport(t, report, "monitor_maintenance")
		assert.Equal(t, "monitor_maintenances", r.Table)
		assert.Equal(t, 4, r.Read)
		assert.Equal(t, 3, r.Copied)
		assert.Equal(t, map[string]int{"maintenance window no longer exists": 1}, r.Skipped)
	})
}
