package mongoimporttest

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LongName is a monitor name that is longer than the 150 characters PostgreSQL keeps.
var LongName = strings.Repeat("n", 160)

// ID is the ObjectID of a document the seed wrote, named by its kind and the name the seed gave it.
func (in *Install) ID(kind, name string) string {
	id, ok := in.ids[kind+":"+name]
	if !ok {
		panic("the seed has no " + kind + " named " + name)
	}
	return id
}

func (in *Install) remember(kind, name, id string) string {
	if in.ids == nil {
		in.ids = map[string]string{}
	}
	in.ids[kind+":"+name] = id
	return id
}

func ptr[T any](v T) *T { return &v }

func (in *Install) seedConfiguration(t *testing.T) {
	t.Helper()
	now := seedNow()
	later := func(d time.Duration) time.Time { return now.Add(d) } // an update after the creation, by whole milliseconds
	missing := primitive.NewObjectID()                             // the ObjectID of a document that was deleted

	// Proxies
	proxyWithAuth, open := primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "proxies", ProxyDocument{ID: proxyWithAuth, Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "proxy-user", Password: "proxy-secret", CreatedAt: now, UpdatedAt: now})
	in.remember("proxy", "auth", proxyWithAuth.Hex())
	in.put(t, "proxies", ProxyDocument{ID: open, Protocol: "socks5", Host: "10.0.0.9", Port: 1080, CreatedAt: now, UpdatedAt: now})
	in.remember("proxy", "open", open.Hex())

	// Monitors: an HTTP one behind a proxy, a push one, a paused one that was saved without a
	// config, and three that a bug or a manual edit left odd. MongoDB doesn't limit the widths
	// PostgreSQL does, or check that the proxy is still there.
	checkout, nightly, paused := primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "monitor", MonitorDocument{
		ID: checkout, Type: "http", Name: "Checkout API", Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 30, ResendInterval: 10,
		Active: true, Status: 1, ProxyID: &proxyWithAuth, CreatedAt: now, UpdatedAt: now,
		Config: `{"url":"https://checkout.example.com/health","method":"GET","accepted_statuscodes":["2XX"]}`,
	})
	in.remember("monitor", "checkout", checkout.Hex())
	in.put(t, "monitor", MonitorDocument{
		ID: nightly, Type: "push", Name: "Nightly job", Interval: 86400, Timeout: 16, MaxRetries: 0, RetryInterval: 60, ResendInterval: 0,
		Active: true, Status: 2, PushToken: "nightly-token", Config: `{"pushToken":"nightly-token"}`, CreatedAt: now, UpdatedAt: now,
	})
	in.remember("monitor", "nightly", nightly.Hex())
	in.put(t, "monitor", MonitorDocument{
		ID: paused, Type: "tcp", Name: "Legacy database", Interval: 120, Timeout: 10, MaxRetries: 1, RetryInterval: 60, ResendInterval: 0,
		Active: false, Status: 0, Config: "", CreatedAt: now, UpdatedAt: now,
	})
	in.remember("monitor", "paused", paused.Hex())
	wide, broken := primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "monitor", MonitorDocument{
		ID: wide, Type: "http", Name: LongName, Interval: 60, Timeout: 16, MaxRetries: 0, RetryInterval: 60, ResendInterval: 0, Active: true,
		Status: 1, Config: `{"url":"https://example.org"}`, ProxyID: &missing, CreatedAt: now, UpdatedAt: now,
	})
	in.remember("monitor", "wide", wide.Hex())
	in.put(t, "monitor", MonitorDocument{
		ID: broken, Type: "http", Name: "Broken config", Interval: 60, Timeout: 16, MaxRetries: 0, RetryInterval: 60, ResendInterval: 0, Active: false,
		Status: 0, Config: "{not json", CreatedAt: now, UpdatedAt: now,
	})
	in.remember("monitor", "broken", broken.Hex())

	// Notification channels. A full update of a channel writes the model's untagged fields, and
	// leaves is_default and updated_at as they were: "Slack" below is a default channel as of its last save.
	webhook, email, slack, teams := primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "notification_channel", NotificationChannelDocument{ID: webhook, Name: "Ops webhook", Type: "webhook", Active: true, Config: ptr(`{"webhook_url":"https://hooks.example/ops"}`), CreatedAt: now, UpdatedAt: now})
	in.remember("channel", "webhook", webhook.Hex())
	in.put(t, "notification_channel", NotificationChannelDocument{ID: email, Name: "Email", Type: "smtp", Active: true, IsDefault: true, Config: ptr(`{"smtp_host":"smtp.example.com"}`), CreatedAt: now, UpdatedAt: now})
	in.remember("channel", "email", email.Hex())
	in.put(t, "notification_channel", NotificationChannelDocument{ID: slack, Name: "Slack ops", Type: "slack", Active: true, Config: ptr(`{"slack_webhook_url":"https://hooks.slack.example/b"}`), CreatedAt: now, UpdatedAt: now})
	in.fullUpdateChannel(t, slack, later(5*time.Millisecond))
	in.remember("channel", "slack", slack.Hex())
	// A channel saved in full and then changed in part: the later change wrote is_default itself, so it wins
	in.put(t, "notification_channel", NotificationChannelDocument{ID: teams, Name: "Teams", Type: "teams", Active: true, Config: ptr(`{"teams_webhook_url":"https://teams.example/a"}`), CreatedAt: now, UpdatedAt: later(10 * time.Millisecond)})
	in.fullUpdateChannel(t, teams, later(5*time.Millisecond))
	in.remember("channel", "teams", teams.Hex())
	in.remember("channel", "bare", in.insert(t, "notification_channel", bson.D{
		{Key: "name", Value: "Bare"}, {Key: "type", Value: "webhook"}, {Key: "active", Value: true}, {Key: "is_default", Value: false},
		{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
	}).Hex())

	// Tags
	production, staging := primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "tags", TagDocument{ID: production, Name: "production", Color: "#EF4444", Description: ptr("Customer facing"), CreatedAt: now, UpdatedAt: now})
	in.remember("tag", "production", production.Hex())
	in.put(t, "tags", TagDocument{ID: staging, Name: "staging", Color: "#22C55E", CreatedAt: now, UpdatedAt: now})
	in.remember("tag", "staging", staging.Hex())
	in.remember("tag", "alpha", in.insert(t, "tags", bson.D{
		{Key: "name", Value: "alpha"}, {Key: "color", Value: "#3B82F6FF"},
		{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
	}).Hex())

	// Links between monitors and channels, tags, maintenance windows and status pages
	for _, link := range [][2]primitive.ObjectID{{checkout, webhook}, {checkout, email}, {nightly, email}} {
		in.put(t, "monitor_notification", MonitorNotificationDocument{ID: primitive.NewObjectID(), MonitorID: link[0], NotificationID: link[1], CreatedAt: now, UpdatedAt: now})
	}
	// A channel was deleted and its links stayed
	in.insert(t, "monitor_notification", bson.D{{Key: "monitor_id", Value: checkout}, {Key: "notification_id", Value: missing}})

	for _, link := range [][2]primitive.ObjectID{{checkout, production}, {checkout, staging}, {nightly, production}} {
		in.put(t, "monitor_tags", MonitorTagDocument{ID: primitive.NewObjectID(), MonitorID: link[0], TagID: link[1], CreatedAt: now, UpdatedAt: now})
	}
	// A monitor was deleted and its link stayed, and so was a tag
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: missing}, {Key: "tag_id", Value: production}})
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: nightly}, {Key: "tag_id", Value: missing}})

	// Maintenance windows. A full update leaves created_at zero, and a partial one or SetActive
	// writes updated_at as an RFC 3339 string.
	weekly, release, nightlyCron := primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "maintenance", MaintenanceDocument{
		ID: weekly, Title: "Weekly patching", Description: "Kernel updates", Active: true, Strategy: "recurring-weekday",
		StartTime: ptr("22:00"), EndTime: ptr("23:30"), Weekdays: []int{1, 3}, Timezone: ptr("Europe/Kyiv"), UpdatedAt: later(5 * time.Millisecond),
	})
	in.remember("maintenance", "weekly", weekly.Hex())
	in.put(t, "maintenance", MaintenanceDocument{
		ID: release, Title: "Release window", Active: false, Strategy: "single", Timezone: ptr("UTC"),
		StartDateTime: ptr("2026-12-01T10:00"), EndDateTime: ptr("2026-12-01T12:00"), CreatedAt: now, UpdatedAt: now,
	})
	in.set(t, "maintenance", release, bson.M{"updated_at": now.Format(time.RFC3339)})
	in.remember("maintenance", "release", release.Hex())
	in.put(t, "maintenance", MaintenanceDocument{
		ID: nightlyCron, Title: "Nightly cron", Active: true, Strategy: "cron", Cron: ptr("0 3 * * *"), Duration: ptr(30), DaysOfMonth: []int{1, 15}, IntervalDay: ptr(2),
		CreatedAt: now, UpdatedAt: now,
	})
	in.set(t, "maintenance", nightlyCron, bson.M{"updated_at": now.Format(time.RFC3339)})
	in.remember("maintenance", "cron", nightlyCron.Hex())

	for _, link := range [][2]primitive.ObjectID{{checkout, weekly}, {nightly, weekly}, {checkout, release}} {
		in.put(t, "monitor_maintenance", MonitorMaintenanceDocument{ID: primitive.NewObjectID(), MonitorID: link[0], MaintenanceID: link[1], CreatedAt: now, UpdatedAt: now})
	}
	in.insert(t, "monitor_maintenance", bson.D{{Key: "monitor_id", Value: checkout}, {Key: "maintenance_id", Value: missing}})

	// Status pages with their monitors in the order the page shows them, one of them hidden,
	// and fields that only MongoDB has
	shop, internal := primitive.NewObjectID(), primitive.NewObjectID()
	in.put(t, "status_pages", StatusPageDocument{
		ID: shop, Slug: "shop", Title: "Shop status", Description: "Is the shop up?", Icon: "https://example.com/icon.png", Theme: "dark",
		Published: true, FooterText: "Thanks", AutoRefreshInterval: 60, CreatedAt: now, UpdatedAt: now,
		SearchEngineIndex: true, Password: "page-secret", GoogleAnalyticsTagID: "G-ABC123",
	})
	in.remember("status_page", "shop", shop.Hex())
	in.put(t, "status_pages", StatusPageDocument{ID: internal, Slug: "internal", Title: "Internal", Theme: "light", AutoRefreshInterval: 300, CreatedAt: now, UpdatedAt: now})
	in.remember("status_page", "internal", internal.Hex())

	for _, link := range []struct {
		page, monitor primitive.ObjectID
		order         int
		active        bool
		created       time.Time
	}{
		{shop, checkout, 2, true, now}, {shop, nightly, 0, true, now}, {shop, paused, 1, false, now},
		// A full update of a link leaves created_at zero
		{internal, checkout, 0, true, time.Time{}},
	} {
		in.put(t, "monitor_status_page", MonitorStatusPageDocument{
			ID: primitive.NewObjectID(), StatusPageID: link.page, MonitorID: link.monitor, Order: link.order, Active: link.active, CreatedAt: link.created, UpdatedAt: now,
		})
	}
	in.insert(t, "monitor_status_page", bson.D{{Key: "status_page_id", Value: missing}, {Key: "monitor_id", Value: checkout}, {Key: "order", Value: 0}, {Key: "active", Value: true}})

	in.put(t, "domain_status_page", DomainStatusPageDocument{ID: primitive.NewObjectID(), StatusPageID: shop, Domain: "status.example.com", CreatedAt: now, UpdatedAt: now})
	in.insert(t, "domain_status_page", bson.D{{Key: "status_page_id", Value: missing}, {Key: "domain", Value: "gone.example.com"}})
}

// fullUpdateChannel adds the keys that a full update of a channel writes, saving the channel as default. The
// update $sets the module's model, which has no bson tags, so it writes name, type, active and config where
// they were, and writes is_default and updated_at, with the model's other fields, under the keys of their Go
// names in lower case. The channel then keeps showing the old is_default and updated_at.
func (in *Install) fullUpdateChannel(t *testing.T, id primitive.ObjectID, at time.Time) {
	t.Helper()
	in.set(t, "notification_channel", id, bson.M{"id": id.Hex(), "isdefault": true, "createdat": time.Time{}, "updatedat": at})
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}
