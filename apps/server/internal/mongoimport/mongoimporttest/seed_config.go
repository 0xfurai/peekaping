package mongoimporttest

import (
	"context"
	"strings"
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
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"

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
	ctx := context.Background()
	missing := primitive.NewObjectID() // the ObjectID of a document that was deleted

	// Proxies
	proxies := proxy.NewMongoRepository(in.Client, in.Config)
	proxyWithAuth, err := proxies.Create(ctx, &proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "proxy-user", Password: "proxy-secret"})
	require.NoError(t, err)
	in.remember("proxy", "auth", proxyWithAuth.ID)
	open, err := proxies.Create(ctx, &proxy.Model{Protocol: "socks5", Host: "10.0.0.9", Port: 1080})
	require.NoError(t, err)
	in.remember("proxy", "open", open.ID)

	// Monitors: an HTTP one behind a proxy, a push one, a paused one that was saved without a
	// config, and three that a bug or a manual edit left odd. MongoDB doesn't limit the widths
	// PostgreSQL does, or check that the proxy is still there.
	monitors := monitor.NewMongoRepository(in.Client, in.Config)
	checkout, err := monitors.Create(ctx, &monitor.Model{
		Type: "http", Name: "Checkout API", Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 30, ResendInterval: 10,
		Active: true, Status: shared.MonitorStatusUp, ProxyId: proxyWithAuth.ID,
		Config: `{"url":"https://checkout.example.com/health","method":"GET","accepted_statuscodes":["2XX"]}`,
	})
	require.NoError(t, err)
	in.remember("monitor", "checkout", checkout.ID)
	nightly, err := monitors.Create(ctx, &monitor.Model{
		Type: "push", Name: "Nightly job", Interval: 86400, Timeout: 16, MaxRetries: 0, RetryInterval: 60, ResendInterval: 0,
		Active: true, Status: shared.MonitorStatusPending, PushToken: "nightly-token", Config: `{"pushToken":"nightly-token"}`,
	})
	require.NoError(t, err)
	in.remember("monitor", "nightly", nightly.ID)
	paused, err := monitors.Create(ctx, &monitor.Model{
		Type: "tcp", Name: "Legacy database", Interval: 120, Timeout: 10, MaxRetries: 1, RetryInterval: 60, ResendInterval: 0,
		Active: false, Status: shared.MonitorStatusDown, Config: "",
	})
	require.NoError(t, err)
	in.remember("monitor", "paused", paused.ID)
	in.remember("monitor", "wide", in.insert(t, "monitor", bson.D{
		{Key: "type", Value: "http"}, {Key: "name", Value: LongName}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
		{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: true},
		{Key: "status", Value: 1}, {Key: "config", Value: `{"url":"https://example.org"}`}, {Key: "proxy_id", Value: missing}, {Key: "push_token", Value: ""},
		{Key: "created_at", Value: time.Now().UTC()}, {Key: "updated_at", Value: time.Now().UTC()},
	}).Hex())
	in.remember("monitor", "broken", in.insert(t, "monitor", bson.D{
		{Key: "type", Value: "http"}, {Key: "name", Value: "Broken config"}, {Key: "interval", Value: 60}, {Key: "timeout", Value: 16},
		{Key: "max_retries", Value: 0}, {Key: "retry_interval", Value: 60}, {Key: "resend_interval", Value: 0}, {Key: "active", Value: false},
		{Key: "status", Value: 0}, {Key: "config", Value: "{not json"}, {Key: "push_token", Value: ""},
		{Key: "created_at", Value: time.Now().UTC()}, {Key: "updated_at", Value: time.Now().UTC()},
	}).Hex())

	// Notification channels. A full update of a channel writes the model's untagged fields, and
	// leaves is_default and updated_at as they were: "Slack" below is a default channel as of its last save.
	channels := notification_channel.NewMongoRepository(in.Client, in.Config)
	webhook, err := channels.Create(ctx, &notification_channel.Model{Name: "Ops webhook", Type: "webhook", Active: true, Config: ptr(`{"webhook_url":"https://hooks.example/ops"}`)})
	require.NoError(t, err)
	in.remember("channel", "webhook", webhook.ID)
	email, err := channels.Create(ctx, &notification_channel.Model{Name: "Email", Type: "smtp", Active: true, IsDefault: true, Config: ptr(`{"smtp_host":"smtp.example.com"}`)})
	require.NoError(t, err)
	in.remember("channel", "email", email.ID)
	slack, err := channels.Create(ctx, &notification_channel.Model{Name: "Slack", Type: "slack", Active: true, Config: ptr(`{"slack_webhook_url":"https://hooks.slack.example/a"}`)})
	require.NoError(t, err)
	in.remember("channel", "slack", slack.ID)
	time.Sleep(5 * time.Millisecond) // so the update is after the creation, which MongoDB keeps to the millisecond
	require.NoError(t, channels.UpdateFull(ctx, slack.ID, &notification_channel.Model{
		ID: slack.ID, Name: "Slack ops", Type: "slack", Active: true, IsDefault: true, Config: ptr(`{"slack_webhook_url":"https://hooks.slack.example/b"}`),
	}))
	// A channel saved in full and then changed in part: the later change wrote is_default itself, so it wins
	teams, err := channels.Create(ctx, &notification_channel.Model{Name: "Teams", Type: "teams", Active: true, Config: ptr(`{"teams_webhook_url":"https://teams.example/a"}`)})
	require.NoError(t, err)
	in.remember("channel", "teams", teams.ID)
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, channels.UpdateFull(ctx, teams.ID, &notification_channel.Model{
		ID: teams.ID, Name: "Teams", Type: "teams", Active: true, IsDefault: true, Config: ptr(`{"teams_webhook_url":"https://teams.example/a"}`),
	}))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, channels.UpdatePartial(ctx, teams.ID, &notification_channel.UpdateModel{IsDefault: ptr(false)}))
	in.remember("channel", "bare", in.insert(t, "notification_channel", bson.D{
		{Key: "name", Value: "Bare"}, {Key: "type", Value: "webhook"}, {Key: "active", Value: true}, {Key: "is_default", Value: false},
		{Key: "created_at", Value: time.Now().UTC()}, {Key: "updated_at", Value: time.Now().UTC()},
	}).Hex())

	// Tags
	tags := tag.NewMongoRepository(in.Client, in.Config)
	production, err := tags.Create(ctx, &tag.Model{Name: "production", Color: "#EF4444", Description: ptr("Customer facing")})
	require.NoError(t, err)
	in.remember("tag", "production", production.ID)
	staging, err := tags.Create(ctx, &tag.Model{Name: "staging", Color: "#22C55E"})
	require.NoError(t, err)
	in.remember("tag", "staging", staging.ID)
	in.remember("tag", "alpha", in.insert(t, "tags", bson.D{
		{Key: "name", Value: "alpha"}, {Key: "color", Value: "#3B82F6FF"},
		{Key: "created_at", Value: time.Now().UTC()}, {Key: "updated_at", Value: time.Now().UTC()},
	}).Hex())

	// Links between monitors and channels, tags, maintenance windows and status pages
	monitorChannels := monitor_notification.NewMongoRepository(in.Client, in.Config)
	for _, link := range [][2]string{{checkout.ID, webhook.ID}, {checkout.ID, email.ID}, {nightly.ID, email.ID}} {
		_, err := monitorChannels.Create(ctx, &monitor_notification.Model{MonitorID: link[0], NotificationID: link[1]})
		require.NoError(t, err)
	}
	// A channel was deleted and its links stayed
	in.insert(t, "monitor_notification", bson.D{{Key: "monitor_id", Value: mustObjectID(t, checkout.ID)}, {Key: "notification_id", Value: missing}})

	monitorTags := monitor_tag.NewMongoRepository(in.Client, in.Config)
	for _, link := range [][2]string{{checkout.ID, production.ID}, {checkout.ID, staging.ID}, {nightly.ID, production.ID}} {
		_, err := monitorTags.Create(ctx, &monitor_tag.Model{MonitorID: link[0], TagID: link[1]})
		require.NoError(t, err)
	}
	// A monitor was deleted and its link stayed, and so was a tag
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: missing}, {Key: "tag_id", Value: mustObjectID(t, production.ID)}})
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: mustObjectID(t, nightly.ID)}, {Key: "tag_id", Value: missing}})

	// Maintenance windows. A full update leaves created_at zero, and a partial one or SetActive
	// writes updated_at as an RFC 3339 string.
	windows := maintenance.NewMongoRepository(in.Client, in.Config)
	weekly, err := windows.Create(ctx, &maintenance.Model{Title: "Weekly patching", Active: true, Strategy: "recurring-weekday"})
	require.NoError(t, err)
	in.remember("maintenance", "weekly", weekly.ID)
	_, err = windows.UpdateFull(ctx, weekly.ID, &maintenance.Model{
		Title: "Weekly patching", Description: "Kernel updates", Active: true, Strategy: "recurring-weekday",
		StartTime: ptr("22:00"), EndTime: ptr("23:30"), Weekdays: []int{1, 3}, Timezone: ptr("Europe/Kyiv"),
	})
	require.NoError(t, err)
	release, err := windows.Create(ctx, &maintenance.Model{
		Title: "Release window", Active: true, Strategy: "single", Timezone: ptr("UTC"),
		StartDateTime: ptr("2026-12-01T10:00"), EndDateTime: ptr("2026-12-01T12:00"),
	})
	require.NoError(t, err)
	in.remember("maintenance", "release", release.ID)
	// SetActive and UpdatePartial write updated_at as a string, which the repository then fails to read
	// back into a time. The update is written all the same.
	_, _ = windows.SetActive(ctx, release.ID, false)
	nightlyCron, err := windows.Create(ctx, &maintenance.Model{Title: "Nightly cron", Active: true, Strategy: "manual"})
	require.NoError(t, err)
	in.remember("maintenance", "cron", nightlyCron.ID)
	_, _ = windows.UpdatePartial(ctx, nightlyCron.ID, &maintenance.UpdateModel{
		Strategy: ptr("cron"), Cron: ptr("0 3 * * *"), Duration: ptr(30), DaysOfMonth: []int{1, 15}, IntervalDay: ptr(2),
	})

	monitorWindows := monitor_maintenance.NewMongoRepository(in.Client, in.Config)
	for _, link := range [][2]string{{checkout.ID, weekly.ID}, {nightly.ID, weekly.ID}, {checkout.ID, release.ID}} {
		_, err := monitorWindows.Create(ctx, &monitor_maintenance.Model{MonitorID: link[0], MaintenanceID: link[1]})
		require.NoError(t, err)
	}
	in.insert(t, "monitor_maintenance", bson.D{{Key: "monitor_id", Value: mustObjectID(t, checkout.ID)}, {Key: "maintenance_id", Value: missing}})

	// Status pages with their monitors in the order the page shows them, one of them hidden,
	// and fields that only MongoDB has
	pages := status_page.NewMongoRepository(in.Client, in.Config)
	shop, err := pages.Create(ctx, &status_page.Model{
		Slug: "shop", Title: "Shop status", Description: "Is the shop up?", Icon: "https://example.com/icon.png", Theme: "dark",
		Published: true, FooterText: "Thanks", AutoRefreshInterval: 60,
	})
	require.NoError(t, err)
	in.remember("status_page", "shop", shop.ID)
	_, err = in.Collection("status_pages").UpdateOne(ctx, bson.M{"_id": mustObjectID(t, shop.ID)}, bson.M{"$set": bson.M{
		"search_engine_index": true, "password": "page-secret", "google_analytics_tag_id": "G-ABC123",
	}})
	require.NoError(t, err)
	internal, err := pages.Create(ctx, &status_page.Model{Slug: "internal", Title: "Internal", Theme: "light", AutoRefreshInterval: 300})
	require.NoError(t, err)
	in.remember("status_page", "internal", internal.ID)

	pageMonitors := monitor_status_page.NewMongoRepository(in.Client, in.Config)
	for _, link := range []struct {
		page, monitor string
		order         int
		active        bool
	}{{shop.ID, checkout.ID, 2, true}, {shop.ID, nightly.ID, 0, true}, {shop.ID, paused.ID, 1, false}, {internal.ID, checkout.ID, 0, true}} {
		_, err := pageMonitors.AddMonitorToStatusPage(ctx, link.page, link.monitor, link.order, link.active)
		require.NoError(t, err)
	}
	// A full update of a link leaves created_at zero
	internalLink, err := pageMonitors.FindByStatusPageAndMonitor(ctx, internal.ID, checkout.ID)
	require.NoError(t, err)
	_, err = pageMonitors.UpdateFull(ctx, internalLink.ID, &monitor_status_page.Model{StatusPageID: internal.ID, MonitorID: checkout.ID, Order: 0, Active: true})
	require.NoError(t, err)
	in.insert(t, "monitor_status_page", bson.D{{Key: "status_page_id", Value: missing}, {Key: "monitor_id", Value: mustObjectID(t, checkout.ID)}, {Key: "order", Value: 0}, {Key: "active", Value: true}})

	domains := domain_status_page.NewMongoRepository(in.Client, in.Config)
	_, err = domains.Create(ctx, &domain_status_page.Model{StatusPageID: shop.ID, Domain: "status.example.com"})
	require.NoError(t, err)
	in.insert(t, "domain_status_page", bson.D{{Key: "status_page_id", Value: missing}, {Key: "domain", Value: "gone.example.com"}})
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}
