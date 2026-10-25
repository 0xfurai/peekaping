package mongoimporttest

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// TestImportSeedMatchesRepositories proves that the seed, which writes raw documents of the frozen
// format, makes the same install as the one the modules' MongoDB repositories made: it seeds one
// database through the repositories, with the seed as it was before they were removed (below), and
// one through Seed, and compares every document of every collection.
//
// ObjectIDs, timestamps, random tokens and hashes differ from one seed to the next, so they are
// replaced by what they stand for before the comparison: a document the seed named by its name,
// an ObjectID it didn't by "unknown", a time made when the seed ran by how many hours it is from
// the seed's start, and a token or hash by its kind once it has the shape of one. Everything else
// must be equal, down to the BSON type of a number and the keys a document has.
//
// This test goes in the pull request that deletes the Mongo repositories, together with them and
// with seedAccountsThroughRepositories and the two seeds that follow it.
func TestImportSeedMatchesRepositories(t *testing.T) {
	through := Empty(t)
	startedThrough := time.Now()
	through.seedAccountsThroughRepositories(t)
	through.seedConfigurationThroughRepositories(t)
	through.seedHistoryThroughRepositories(t)

	startedRaw := time.Now()
	raw := Seed(t)

	want, got := through.normalizedInstall(t, startedThrough), raw.normalizedInstall(t, startedRaw)
	collections := slices.Sorted(maps.Keys(want))
	require.Equal(t, collections, slices.Sorted(maps.Keys(got)), "the collections of the install")
	require.Len(t, collections, 20, "an install has a collection for each of the 20 kinds of document the import reads")
	for _, collection := range collections {
		assert.NotEmpty(t, want[collection], collection)
		assert.Equal(t, want[collection], got[collection], collection)
	}
}

// Time keys: the documents made by a running install carry the time they were made at under these keys.
var madeAtKeys = map[string]bool{
	"created_at": true, "updated_at": true, "createdAt": true, "updatedAt": true, "createdat": true, "updatedat": true,
	"last_used": true, "expires_at": true,
}

var (
	objectIDHex    = regexp.MustCompile(`^[0-9a-f]{24}$`)
	bcryptHash     = regexp.MustCompile(`^\$2[aby]\$\d\d\$[./A-Za-z0-9]{53}$`)
	signingSecret  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sha256KeyHash  = regexp.MustCompile(`^sha256\$[0-9a-f]{64}$`)
	maskedAPIToken = regexp.MustCompile(`^pk_.{5}\.\.\..{4}$`)
)

// normalizedInstall is every document of the install, by collection, with what differs from one
// seed to the next replaced: see TestImportSeedMatchesRepositories. startedAt is when the seed began.
// The documents of a collection are in the order of their text, since their ObjectIDs are gone.
func (in *Install) normalizedInstall(t *testing.T, startedAt time.Time) map[string][]bson.M {
	t.Helper()
	ctx := context.Background()
	names := map[string]string{in.AdminID: "admin", in.APIKeyID: "api key CI"}
	for name, id := range in.ids {
		names[id] = name
	}

	collections, err := in.Client.Database(in.Database).ListCollectionNames(ctx, bson.D{})
	require.NoError(t, err)
	out := map[string][]bson.M{}
	for _, collection := range collections {
		cursor, err := in.Collection(collection).Find(ctx, bson.D{})
		require.NoError(t, err)
		var documents []bson.M
		require.NoError(t, cursor.All(ctx, &documents))
		for _, document := range documents {
			normalized := map[string]any{}
			for key, value := range document {
				normalized[key] = normalizeValue(key, value, names, startedAt)
			}
			normalizeSecrets(collection, document, normalized)
			out[collection] = append(out[collection], normalized)
		}
		slices.SortFunc(out[collection], func(a, b bson.M) int {
			x, y := fmt.Sprint(a), fmt.Sprint(b)
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		})
	}
	return out
}

func normalizeValue(key string, value any, names map[string]string, startedAt time.Time) any {
	hoursFromStart := func(at time.Time) string {
		if at.IsZero() {
			return "zero time"
		}
		return "start " + at.Sub(startedAt).Round(time.Hour).String()
	}
	switch v := value.(type) {
	case primitive.ObjectID:
		if name, ok := names[v.Hex()]; ok {
			return name
		}
		return "unknown ObjectID"
	case primitive.DateTime:
		if madeAtKeys[key] {
			return hoursFromStart(v.Time())
		}
	case string:
		if objectIDHex.MatchString(v) {
			if name, ok := names[v]; ok {
				return name
			}
			return "unknown ObjectID"
		}
		if at, err := time.Parse(time.RFC3339, v); err == nil && madeAtKeys[key] {
			return "RFC 3339 string, " + hoursFromStart(at)
		}
	case primitive.A:
		out := make(primitive.A, len(v))
		for i, item := range v {
			out[i] = normalizeValue(key, item, names, startedAt)
		}
		return out
	}
	return value
}

// normalizeSecrets replaces the random tokens and hashes of a document that have the shape of one.
func normalizeSecrets(collection string, document, normalized bson.M) {
	replace := func(key string, shape *regexp.Regexp, kind string) {
		if text, ok := document[key].(string); ok && shape.MatchString(text) {
			normalized[key] = kind
		}
	}
	switch collection {
	case "users":
		replace("password", bcryptHash, "bcrypt hash")
	case "setting":
		if key := document["key"]; key == accessTokenSecretKey || key == refreshTokenSecretKey {
			replace("value", signingSecret, "signing secret")
		}
	case "api_keys":
		replace("key_hash", sha256KeyHash, "SHA-256 hash")
		replace("display_key", maskedAPIToken, "masked token")
	}
}

// seedAccountsThroughRepositories is seedAccounts as it was written before the repositories were removed.
func (in *Install) seedAccountsThroughRepositories(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cfg, logger := &config.Config{DBName: in.Database}, zap.NewNop().Sugar()

	// The admin, with two-factor authentication on
	hash, err := bcrypt.GenerateFromPassword([]byte(AdminPassword), bcrypt.MinCost)
	require.NoError(t, err)
	admin, err := auth.NewMongoRepository(in.Client, cfg).Create(ctx, &auth.Model{
		Email: AdminEmail, Password: string(hash), Active: true,
		TwoFASecret: AdminTOTPSecret, TwoFAStatus: true, TwoFALastToken: "123456",
	})
	require.NoError(t, err)
	in.AdminID = admin.ID

	// A user from before the createdAt and updatedAt keys, and a second document for its email
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: string(hash)}, {Key: "active", Value: true}})
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: "other"}, {Key: "active", Value: false}})

	// The settings a first start writes, which never set created_at, and the one an operator changes.
	// The retention setting also has an older document of the same key.
	settings := setting.NewService(setting.NewMongoRepository(in.Client, cfg), logger)
	require.NoError(t, settings.InitializeSettings(ctx))
	_, err = settings.SetByKey(ctx, setting.DataRetentionPeriodKey, &setting.Model{Value: RetentionDays, Type: "int"})
	require.NoError(t, err)
	in.insert(t, "setting", bson.D{
		{Key: "key", Value: setting.DataRetentionPeriodKey}, {Key: "value", Value: "7"}, {Key: "type", Value: "int"},
		{Key: "updated_at", Value: time.Now().Add(-48 * time.Hour).UTC()},
	})

	// API keys: one that has been used, and one with an expiry and a usage limit
	keys := api_key.NewService(api_key.NewMongoRepository(in.Client, cfg), logger)
	used, err := keys.Create(ctx, &api_key.CreateRequest{Name: "CI"})
	require.NoError(t, err)
	_, err = keys.ValidateKey(ctx, used.Token)
	require.NoError(t, err)
	in.APIKeyID, in.APIKeyToken = used.ID, used.Token
	expires, limit := time.Now().Add(72*time.Hour).UTC().Truncate(time.Millisecond), int64(100)
	_, err = keys.Create(ctx, &api_key.CreateRequest{Name: "Dashboard", ExpiresAt: &expires, MaxUsageCount: &limit})
	require.NoError(t, err)
}

// seedConfigurationThroughRepositories is seedConfiguration as it was written before the repositories were removed.
func (in *Install) seedConfigurationThroughRepositories(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cfg := &config.Config{DBName: in.Database}
	missing := primitive.NewObjectID() // the ObjectID of a document that was deleted

	// Proxies
	proxies := proxy.NewMongoRepository(in.Client, cfg)
	proxyWithAuth, err := proxies.Create(ctx, &proxy.Model{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "proxy-user", Password: "proxy-secret"})
	require.NoError(t, err)
	in.remember("proxy", "auth", proxyWithAuth.ID)
	open, err := proxies.Create(ctx, &proxy.Model{Protocol: "socks5", Host: "10.0.0.9", Port: 1080})
	require.NoError(t, err)
	in.remember("proxy", "open", open.ID)

	// Monitors: an HTTP one behind a proxy, a push one, a paused one that was saved without a
	// config, and three that a bug or a manual edit left odd. MongoDB doesn't limit the widths
	// PostgreSQL does, or check that the proxy is still there.
	monitors := monitor.NewMongoRepository(in.Client, cfg)
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
	channels := notification_channel.NewMongoRepository(in.Client, cfg)
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
	tags := tag.NewMongoRepository(in.Client, cfg)
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
	monitorChannels := monitor_notification.NewMongoRepository(in.Client, cfg)
	for _, link := range [][2]string{{checkout.ID, webhook.ID}, {checkout.ID, email.ID}, {nightly.ID, email.ID}} {
		_, err := monitorChannels.Create(ctx, &monitor_notification.Model{MonitorID: link[0], NotificationID: link[1]})
		require.NoError(t, err)
	}
	// A channel was deleted and its links stayed
	in.insert(t, "monitor_notification", bson.D{{Key: "monitor_id", Value: mustObjectID(t, checkout.ID)}, {Key: "notification_id", Value: missing}})

	monitorTags := monitor_tag.NewMongoRepository(in.Client, cfg)
	for _, link := range [][2]string{{checkout.ID, production.ID}, {checkout.ID, staging.ID}, {nightly.ID, production.ID}} {
		_, err := monitorTags.Create(ctx, &monitor_tag.Model{MonitorID: link[0], TagID: link[1]})
		require.NoError(t, err)
	}
	// A monitor was deleted and its link stayed, and so was a tag
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: missing}, {Key: "tag_id", Value: mustObjectID(t, production.ID)}})
	in.insert(t, "monitor_tags", bson.D{{Key: "monitor_id", Value: mustObjectID(t, nightly.ID)}, {Key: "tag_id", Value: missing}})

	// Maintenance windows. A full update leaves created_at zero, and a partial one or SetActive
	// writes updated_at as an RFC 3339 string.
	windows := maintenance.NewMongoRepository(in.Client, cfg)
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

	monitorWindows := monitor_maintenance.NewMongoRepository(in.Client, cfg)
	for _, link := range [][2]string{{checkout.ID, weekly.ID}, {nightly.ID, weekly.ID}, {checkout.ID, release.ID}} {
		_, err := monitorWindows.Create(ctx, &monitor_maintenance.Model{MonitorID: link[0], MaintenanceID: link[1]})
		require.NoError(t, err)
	}
	in.insert(t, "monitor_maintenance", bson.D{{Key: "monitor_id", Value: mustObjectID(t, checkout.ID)}, {Key: "maintenance_id", Value: missing}})

	// Status pages with their monitors in the order the page shows them, one of them hidden,
	// and fields that only MongoDB has
	pages := status_page.NewMongoRepository(in.Client, cfg)
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

	pageMonitors := monitor_status_page.NewMongoRepository(in.Client, cfg)
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

	domains := domain_status_page.NewMongoRepository(in.Client, cfg)
	_, err = domains.Create(ctx, &domain_status_page.Model{StatusPageID: shop.ID, Domain: "status.example.com"})
	require.NoError(t, err)
	in.insert(t, "domain_status_page", bson.D{{Key: "status_page_id", Value: missing}, {Key: "domain", Value: "gone.example.com"}})
}

// seedHistoryThroughRepositories is seedHistory as it was written before the repositories were removed.
func (in *Install) seedHistoryThroughRepositories(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cfg, logger := &config.Config{DBName: in.Database}, zap.NewNop().Sugar()
	checkout, nightly := in.ID("monitor", "checkout"), in.ID("monitor", "nightly")
	missing := primitive.NewObjectID() // a monitor that was deleted

	// Heartbeats: the first and the sixth have an end time, and the others have a zero one
	heartbeats := heartbeat.NewMongoRepository(in.Client, cfg)
	for i, h := range []heartbeat.Model{
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 120, Duration: 60, Important: true, Notified: true, Time: HistoryStart, EndTime: HistoryStart.Add(time.Minute)},
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 80, Duration: 60, Time: HistoryStart.Add(time.Minute)},
		{Status: shared.MonitorStatusDown, Msg: "connection refused", Duration: 60, DownCount: 3, Retries: 3, Important: true, Time: HistoryStart.Add(2 * time.Minute)},
		{Status: shared.MonitorStatusMaintenance, Msg: "In maintenance", Time: HistoryStart.Add(3 * time.Minute)},
		{Status: shared.MonitorStatusPending, Msg: "timeout", Retries: 1, Time: HistoryStart.Add(4 * time.Minute)},
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 95, Duration: 3600, Time: HistoryStart.Add(25 * time.Hour), EndTime: HistoryStart.Add(26 * time.Hour)},
	} {
		h.MonitorID = checkout
		created, err := heartbeats.Create(ctx, &h)
		require.NoError(t, err)
		in.remember("heartbeat", HeartbeatNames[i], created.ID)
	}
	// One that never got a time
	in.remember("heartbeat", HeartbeatNames[len(HeartbeatNames)-1], in.insert(t, "heartbeat", bson.D{
		{Key: "monitor_id", Value: mustObjectID(t, checkout)}, {Key: "status", Value: 1}, {Key: "msg", Value: "200 - OK"}, {Key: "ping", Value: 70},
		{Key: "duration", Value: 0}, {Key: "down_count", Value: 0}, {Key: "retries", Value: 0}, {Key: "important", Value: false}, {Key: "notified", Value: false},
	}).Hex())
	// One of a monitor that was deleted, which MongoDB doesn't cascade to
	in.insert(t, "heartbeat", bson.D{
		{Key: "monitor_id", Value: missing}, {Key: "status", Value: 1}, {Key: "msg", Value: "ok"}, {Key: "ping", Value: 1}, {Key: "time", Value: HistoryStart},
	})

	// Stats, aggregated by the stats service as a running install does
	stat := stats.NewService(stats.NewMongoRepository(in.Client, cfg), logger)
	for _, sample := range StatSamples {
		require.NoError(t, stat.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{
			MonitorID: checkout, Status: int(sample.Status), Ping: sample.Ping, Time: HistoryStart.Add(sample.After).Unix(),
		}))
	}
	// Two documents for one minute of the "nightly" monitor, which a race between the first two heartbeats of a
	// bucket could leave. The counts are 2 + 3 up of which 1 maintenance, 1 down, and 2 + 2 response times
	// averaging 100 and 60 ms, so the minute is 5 up, 1 down, 1 maintenance and an average of 80 ms over 4 checks.
	minute := HistoryStart.Add(3 * time.Hour)
	statDocument := func(monitorID primitive.ObjectID, at time.Time, up, down, maintenance int, ping, min, max float64) bson.D {
		return bson.D{
			{Key: "monitor_id", Value: monitorID}, {Key: "timestamp", Value: at}, {Key: "up", Value: up}, {Key: "down", Value: down},
			{Key: "maintenance", Value: maintenance}, {Key: "ping", Value: ping}, {Key: "ping_min", Value: min}, {Key: "ping_max", Value: max},
		}
	}
	nightlyID := mustObjectID(t, nightly)
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute, 2, 1, 0, 100, 90, 110))
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute, 3, 0, 1, 60, 40, 90))
	// More maintenance checks than up ones, which can't be: the response times count nothing
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute.Add(time.Minute), 1, 0, 2, 0, 0, 0))
	// Of a monitor that was deleted, and one without a time
	in.insert(t, "stat_minutely", statDocument(missing, minute, 1, 0, 0, 10, 10, 10))
	in.insert(t, "stat_hourly", bson.D{{Key: "monitor_id", Value: nightlyID}, {Key: "up", Value: 1}, {Key: "down", Value: 0}, {Key: "maintenance", Value: 0}, {Key: "ping", Value: 5}})

	// TLS information and certificate expiry notifications, which refer to the monitor by a string
	tls := monitor_tls_info.NewMongoRepository(in.Client, cfg)
	_, err := tls.Create(ctx, &monitor_tls_info.Model{MonitorID: checkout, InfoJSON: `{"valid":true,"certInfo":{"subject":"checkout.example.com"}}`})
	require.NoError(t, err)
	_, err = tls.Create(ctx, &monitor_tls_info.Model{MonitorID: missing.Hex(), InfoJSON: `{"valid":false}`})
	require.NoError(t, err)
	sent := notification_sent_history.NewMongoRepository(in.Client, cfg)
	for _, record := range []notification_sent_history.Model{{Type: "certificate", MonitorID: checkout, Days: 14}, {Type: "certificate", MonitorID: checkout, Days: 7}, {Type: "certificate", MonitorID: missing.Hex(), Days: 7}} {
		require.NoError(t, sent.RecordSent(ctx, &record))
	}
}
