package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/requests")

// The notification the golden cases send. Times are UTC, so formatted times don't depend on the machine.
var (
	goldenMonitor = &monitor.Model{
		ID:           "mon-1",
		Type:         "http",
		Name:         "Public API",
		Interval:     60,
		Timeout:      16,
		MaxRetries:   3,
		Active:       true,
		Status:       shared.MonitorStatusDown,
		Config:       `{"url":"https://api.example.com/health","method":"GET"}`,
		LocationRule: shared.LocationRuleAny,
		ProbeIDs:     []string{"local"},
		Locations:    []shared.MonitorLocation{{ProbeID: "local", ProbeName: "Local", Online: true}},
		Tags:         []string{"severity:critical", "team:core"},
		CreatedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	goldenDown = &heartbeat.Model{
		ID:        "hb-1",
		MonitorID: "mon-1",
		ProbeID:   "local",
		Status:    shared.MonitorStatusDown,
		Msg:       "connection refused",
		Important: true,
		Time:      time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC),
	}
	goldenUp = &heartbeat.Model{
		ID:        "hb-2",
		MonitorID: "mon-1",
		ProbeID:   "local",
		Status:    shared.MonitorStatusUp,
		Msg:       "200 - OK",
		Ping:      42,
		Important: true,
		Time:      time.Date(2026, 10, 17, 9, 35, 0, 0, time.UTC),
	}
	// goldenProbeOffline and goldenProbeOnline are the stand-in monitors the listener sends a
	// probe's notification with: Down when the probe went offline, Up when it came back online
	goldenProbeOffline = &monitor.Model{ID: "probe-1", Type: shared.MonitorTypeProbe, Name: "Probe Frankfurt", Status: shared.MonitorStatusDown}
	goldenProbeOnline  = &monitor.Model{ID: "probe-1", Type: shared.MonitorTypeProbe, Name: "Probe Frankfurt", Status: shared.MonitorStatusUp}
)

// The messages the listener builds for a probe going offline and coming back online
const (
	goldenProbeOfflineMessage = "Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC"
	goldenProbeOnlineMessage  = "Probe Frankfurt is online"
)

// goldenCertMessage is the message the listener builds for a certificate expiry notification
const goldenCertMessage = "🚨 Certificate Expiry Warning\n\n" +
	"Monitor: Public API\n" +
	"Certificate: api.example.com (server)\n" +
	"Expires in: 7 days\n" +
	"Valid until: 2026-10-24 09:30:00\n" +
	"Notification threshold: 7 days\n" +
	"Valid for: api.example.com\n" +
	"Issuer: CN=R11,O=Let's Encrypt,C=US"

// goldenEvent is what triggers a golden case's notification
type goldenEvent int

const (
	// downEvent is an important heartbeat that takes the monitor Down
	downEvent goldenEvent = iota
	// upEvent is an important heartbeat that brings the monitor back Up
	upEvent
	// certExpiryEvent is a certificate expiry notification, which carries no heartbeat
	certExpiryEvent
	// probeOfflineEvent is a probe going offline, sent with a stand-in monitor and no heartbeat
	probeOfflineEvent
	// probeOnlineEvent is a probe coming back online, sent with a stand-in monitor and no heartbeat
	probeOnlineEvent
)

// goldenSender is the part of a notification channel provider the golden tests use
type goldenSender interface {
	Validate(configJSON string) error
	Send(ctx context.Context, configJSON, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) error
}

type goldenCase struct {
	// name is the golden file, testdata/requests/<name>.golden. It starts with the channel type.
	name   string
	sender func(client *http.Client) goldenSender
	config string
	event  goldenEvent
	// response is the body the fake endpoint answers with
	response string
}

func nopLogger() *zap.SugaredLogger {
	return zap.NewNop().Sugar()
}

// goldenAppConfig sets the URL that channels link back to Peekaping with
var goldenAppConfig = &config.Config{ClientURL: "https://peekaping.example.com/"}

var (
	discord       = func(c *http.Client) goldenSender { return NewDiscordSender(nopLogger(), c) }
	googleChat    = func(c *http.Client) goldenSender { return NewGoogleChatSender(nopLogger(), goldenAppConfig, c) }
	gotify        = func(c *http.Client) goldenSender { return NewGotifySender(nopLogger(), c) }
	grafanaOncall = func(c *http.Client) goldenSender { return NewGrafanaOncallSender(nopLogger(), c) }
	line          = func(c *http.Client) goldenSender { return NewLineSender(nopLogger(), c) }
	matrix        = func(c *http.Client) goldenSender { return NewMatrixSender(nopLogger(), c) }
	mattermost    = func(c *http.Client) goldenSender { return NewMattermostSender(nopLogger(), c) }
	ntfy          = func(c *http.Client) goldenSender { return NewNTFYSender(nopLogger(), c) }
	opsgenie      = func(c *http.Client) goldenSender { return NewOpsgenieSender(nopLogger(), c) }
	pagerDuty     = func(c *http.Client) goldenSender { return NewPagerDutySender(nopLogger(), goldenAppConfig, c) }
	pagerTree     = func(c *http.Client) goldenSender { return NewPagerTreeSender(nopLogger(), c) }
	pushbullet    = func(c *http.Client) goldenSender { return NewPushbulletSender(nopLogger(), c) }
	pushover      = func(c *http.Client) goldenSender { return NewPushoverSender(nopLogger(), c) }
	sendGrid      = func(c *http.Client) goldenSender { return NewSendGridSender(nopLogger(), c) }
	signal        = func(c *http.Client) goldenSender { return NewSignalSender(nopLogger(), c) }
	slack         = func(c *http.Client) goldenSender { return NewSlackSender(nopLogger(), goldenAppConfig, c) }
	teams         = func(c *http.Client) goldenSender { return NewTeamsSender(nopLogger(), goldenAppConfig, c) }
	telegram      = func(c *http.Client) goldenSender { return NewTelegramSender(nopLogger(), c) }
	twilio        = func(c *http.Client) goldenSender { return NewTwilioSender(nopLogger(), c) }
	webhook       = func(c *http.Client) goldenSender { return NewWebhookSender(nopLogger(), c) }
	weCom         = func(c *http.Client) goldenSender { return NewWeComSender(nopLogger(), c) }
	whatsApp      = func(c *http.Client) goldenSender { return NewWhatsAppSender(nopLogger(), c) }
)

var goldenCases = []goldenCase{
	{name: "discord_down", sender: discord, event: downEvent,
		config: `{"webhook_url": "https://discord.com/api/webhooks/123/token", "bot_display_name": "Peekaping", "custom_message_prefix": "@here"}`},
	{name: "discord_thread_up", sender: discord, event: upEvent,
		config: `{"webhook_url": "https://discord.com/api/webhooks/123/token", "message_type": "send_to_thread", "thread_id": "456"}`},
	{name: "discord_forum_post_down", sender: discord, event: downEvent,
		config: `{"webhook_url": "https://discord.com/api/webhooks/123/token", "message_type": "send_to_new_forum_post", "thread_name": "API outage"}`},
	{name: "discord_certificate_expiry", sender: discord, event: certExpiryEvent,
		config: `{"webhook_url": "https://discord.com/api/webhooks/123/token"}`},

	{name: "google_chat_down", sender: googleChat, event: downEvent,
		config: `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages?key=k&token=t"}`},
	{name: "google_chat_up", sender: googleChat, event: upEvent,
		config: `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages?key=k&token=t"}`},
	{name: "google_chat_certificate_expiry", sender: googleChat, event: certExpiryEvent,
		config: `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages?key=k&token=t"}`},
	// A probe's notification links to the Probes page, since the stand-in monitor has no page
	{name: "google_chat_probe_offline", sender: googleChat, event: probeOfflineEvent,
		config: `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages?key=k&token=t"}`},

	{name: "gotify_down", sender: gotify, event: downEvent,
		config: `{"server_url": "https://gotify.example.com/", "application_token": "app-token"}`},
	{name: "gotify_templates_up", sender: gotify, event: upEvent,
		config: `{"server_url": "https://gotify.example.com", "application_token": "app-token", "priority": 3, "title": "{{ name }} is {{ status }}", "custom_message": "{{ status_icon }} {{ msg }} in {{ ping }} ms"}`},
	{name: "gotify_certificate_expiry", sender: gotify, event: certExpiryEvent,
		config: `{"server_url": "https://gotify.example.com/", "application_token": "app-token"}`},

	{name: "grafana_oncall_down", sender: grafanaOncall, event: downEvent,
		config: `{"grafana_oncall_url": "https://oncall.example.com/integrations/v1/formatted_webhook/abc/"}`},
	{name: "grafana_oncall_up", sender: grafanaOncall, event: upEvent,
		config: `{"grafana_oncall_url": "https://oncall.example.com/integrations/v1/formatted_webhook/abc/"}`},
	// The alert uid is the probe's, so the online notification resolves the offline one
	{name: "grafana_oncall_probe_offline", sender: grafanaOncall, event: probeOfflineEvent,
		config: `{"grafana_oncall_url": "https://oncall.example.com/integrations/v1/formatted_webhook/abc/"}`},
	{name: "grafana_oncall_probe_online", sender: grafanaOncall, event: probeOnlineEvent,
		config: `{"grafana_oncall_url": "https://oncall.example.com/integrations/v1/formatted_webhook/abc/"}`},
	// Known issue: without a heartbeat the alert is titled "General notification"
	{name: "grafana_oncall_certificate_expiry", sender: grafanaOncall, event: certExpiryEvent,
		config: `{"grafana_oncall_url": "https://oncall.example.com/integrations/v1/formatted_webhook/abc/"}`},

	{name: "line_down", sender: line, event: downEvent,
		config: `{"channel_access_token": "line-token", "user_id": "U123"}`},
	{name: "line_up", sender: line, event: upEvent,
		config: `{"channel_access_token": "line-token", "user_id": "U123"}`},
	{name: "line_template_down", sender: line, event: downEvent,
		config: `{"channel_access_token": "line-token", "user_id": "U123", "template": "{{ status_icon }} {{ name }}: {{ msg }}"}`},
	{name: "line_certificate_expiry", sender: line, event: certExpiryEvent,
		config: `{"channel_access_token": "line-token", "user_id": "U123"}`},

	{name: "matrix_down", sender: matrix, event: downEvent,
		config: `{"homeserver_url": "https://matrix.example.com", "internal_room_id": "!room:example.com", "access_token": "matrix-token"}`},
	{name: "matrix_custom_message_up", sender: matrix, event: upEvent,
		config: `{"homeserver_url": "https://matrix.example.com", "internal_room_id": "!room:example.com", "access_token": "matrix-token", "custom_message": "{{ name }} is {{ status }}"}`},
	{name: "matrix_certificate_expiry", sender: matrix, event: certExpiryEvent,
		config: `{"homeserver_url": "https://matrix.example.com", "internal_room_id": "!room:example.com", "access_token": "matrix-token"}`},

	{name: "mattermost_down", sender: mattermost, event: downEvent,
		config: `{"webhook_url": "https://mattermost.example.com/hooks/abc", "username": "Peekaping", "channel": "Alerts", "icon_emoji": ":white_check_mark: :x:"}`},
	{name: "mattermost_up", sender: mattermost, event: upEvent,
		config: `{"webhook_url": "https://mattermost.example.com/hooks/abc", "icon_url": "https://peekaping.example.com/icon.png"}`},
	{name: "mattermost_template_down", sender: mattermost, event: downEvent,
		config: `{"webhook_url": "https://mattermost.example.com/hooks/abc", "use_template": true, "template": "{{ name }} is {{ status }}: {{ msg }}"}`},
	{name: "mattermost_certificate_expiry", sender: mattermost, event: certExpiryEvent,
		config: `{"webhook_url": "https://mattermost.example.com/hooks/abc"}`},

	{name: "ntfy_down", sender: ntfy, event: downEvent,
		config: `{"server_url": "https://ntfy.example.com/", "topic": "peekaping", "authentication_type": "none", "priority": 5}`},
	{name: "ntfy_basic_auth_up", sender: ntfy, event: upEvent,
		config: `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "basic", "username": "ntfy-user", "password": "ntfy-pass", "priority": 3, "title": "{{ name }} is {{ status }}", "tags": "{{ status }}, peekaping"}`},
	{name: "ntfy_token_certificate_expiry", sender: ntfy, event: certExpiryEvent,
		config: `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "token", "token": "tk_ntfy", "priority": 4}`},

	{name: "opsgenie_down", sender: opsgenie, event: downEvent,
		config: `{"region": "us", "api_key": "genie-key", "priority": 2}`},
	{name: "opsgenie_eu_up", sender: opsgenie, event: upEvent,
		config: `{"region": "eu", "api_key": "genie-key", "priority": 2}`},
	// The alias is the probe's, not the test alert's, and the channel's priority applies
	{name: "opsgenie_probe_offline", sender: opsgenie, event: probeOfflineEvent,
		config: `{"region": "us", "api_key": "genie-key", "priority": 2}`},
	// The probe coming back online closes the alert its offline notification opened
	{name: "opsgenie_probe_online", sender: opsgenie, event: probeOnlineEvent,
		config: `{"region": "eu", "api_key": "genie-key", "priority": 2}`},
	// Known issue: without a heartbeat Opsgenie sends a test alert, with the test alias and
	// P5, so every monitor's certificate expiry notification merges into one alert
	{name: "opsgenie_certificate_expiry", sender: opsgenie, event: certExpiryEvent,
		config: `{"region": "us", "api_key": "genie-key", "priority": 3}`},

	{name: "pagerduty_down", sender: pagerDuty, event: downEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue", "pagerduty_priority": "critical"}`},
	{name: "pagerduty_up_resolve", sender: pagerDuty, event: upEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue", "pagerduty_auto_resolve": "resolve"}`},
	{name: "pagerduty_up_without_auto_resolve", sender: pagerDuty, event: upEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`},
	// The dedup key is the probe's, and client_url is the Probes page
	{name: "pagerduty_probe_offline", sender: pagerDuty, event: probeOfflineEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`},
	// The probe coming back online resolves the incident its offline notification opened
	{name: "pagerduty_probe_online_resolve", sender: pagerDuty, event: probeOnlineEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue", "pagerduty_auto_resolve": "resolve"}`},
	// Set to do nothing, as a monitor's Up does: no second trigger, and the incident stays open
	{name: "pagerduty_probe_online_without_auto_resolve", sender: pagerDuty, event: probeOnlineEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue", "pagerduty_auto_resolve": "0"}`},
	// Known issue: the dedup key is the monitor's, so this merges into an open Down incident
	// and the next Up resolves it
	{name: "pagerduty_certificate_expiry", sender: pagerDuty, event: certExpiryEvent,
		config: `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`},

	// Known issue: alert IDs end with the send time, so a resolve can't match its create
	{name: "pagertree_down", sender: pagerTree, event: downEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123", "urgency": "high", "authToken": "tree-token"}`},
	{name: "pagertree_up_resolve", sender: pagerTree, event: upEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123", "autoResolve": true}`},
	{name: "pagertree_up_without_auto_resolve", sender: pagerTree, event: upEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`},
	// The alert ID is the probe's, with no send time, so the online notification can resolve it
	{name: "pagertree_probe_offline", sender: pagerTree, event: probeOfflineEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`},
	{name: "pagertree_probe_online_resolve", sender: pagerTree, event: probeOnlineEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123", "autoResolve": true}`},
	{name: "pagertree_probe_online_without_auto_resolve", sender: pagerTree, event: probeOnlineEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`},
	{name: "pagertree_certificate_expiry", sender: pagerTree, event: certExpiryEvent,
		config: `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`},

	{name: "pushbullet_down", sender: pushbullet, event: downEvent,
		config: `{"pushbullet_access_token": "o.pushbullet"}`},
	{name: "pushbullet_device_up", sender: pushbullet, event: upEvent,
		config: `{"pushbullet_access_token": "o.pushbullet", "pushbullet_device_id": "device-1", "pushbullet_channel_tag": "ops"}`},
	{name: "pushbullet_template_down", sender: pushbullet, event: downEvent,
		config: `{"pushbullet_access_token": "o.pushbullet", "pushbullet_custom_template": "{{ name }}: {{ msg }}"}`},
	{name: "pushbullet_certificate_expiry", sender: pushbullet, event: certExpiryEvent,
		config: `{"pushbullet_access_token": "o.pushbullet"}`},

	{name: "pushover_down", sender: pushover, event: downEvent,
		config: `{"pushover_user_key": "user-key", "pushover_app_token": "app-token", "pushover_priority": 1, "pushover_sounds": "siren", "pushover_sounds_up": "magic"}`},
	{name: "pushover_up", sender: pushover, event: upEvent,
		config: `{"pushover_user_key": "user-key", "pushover_app_token": "app-token", "pushover_device": "phone", "pushover_title": "Peekaping", "pushover_sounds": "siren", "pushover_sounds_up": "magic", "pushover_ttl": 3600}`},
	{name: "pushover_certificate_expiry", sender: pushover, event: certExpiryEvent,
		config: `{"pushover_user_key": "user-key", "pushover_app_token": "app-token"}`},

	{name: "sendgrid_down", sender: sendGrid, event: downEvent,
		config: `{"api_key": "SG.key", "from_email": "alerts@example.com", "to_email": "oncall@example.com", "cc_email": "a@example.com, b@example.com", "bcc_email": "c@example.com", "subject": "{{ name }} is {{ status }}"}`},
	{name: "sendgrid_certificate_expiry", sender: sendGrid, event: certExpiryEvent,
		config: `{"api_key": "SG.key", "from_email": "alerts@example.com", "to_email": "oncall@example.com"}`},

	{name: "signal_down", sender: signal, event: downEvent,
		config: `{"signal_url": "https://signal.example.com/v2/send", "signal_number": "+15550000001", "signal_recipients": "+15550000002, +15550000003"}`},
	{name: "signal_custom_message_up", sender: signal, event: upEvent,
		config: `{"signal_url": "https://signal.example.com/v2/send", "signal_number": "+15550000001", "signal_recipients": "+15550000002", "custom_message": "{{ status_icon }} {{ name }} is {{ status }}"}`},
	{name: "signal_certificate_expiry", sender: signal, event: certExpiryEvent,
		config: `{"signal_url": "https://signal.example.com/v2/send", "signal_number": "+15550000001", "signal_recipients": "+15550000002"}`},

	{name: "slack_webhook_down", sender: slack, event: downEvent,
		config: `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X", "slack_username": "Peekaping", "slack_icon_emoji": ":rotating_light:", "slack_channel_notify": true}`},
	{name: "slack_bot_rich_up", sender: slack, event: upEvent, response: `{"ok": true}`,
		config: `{"slack_bot_token": "xoxb-token", "slack_channel": "#alerts", "slack_rich_message": true}`},
	{name: "slack_webhook_template_down", sender: slack, event: downEvent,
		config: `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X", "use_template": true, "template": "{{ status_icon }} {{ name }}: {{ msg }}"}`},
	{name: "slack_webhook_rich_certificate_expiry", sender: slack, event: certExpiryEvent,
		config: `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X", "slack_rich_message": true}`},

	// A probe's notification links to the Probes page, since the stand-in monitor has no page
	{name: "slack_webhook_rich_probe_offline", sender: slack, event: probeOfflineEvent,
		config: `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X", "slack_rich_message": true}`},
	{name: "slack_bot_rich_probe_online", sender: slack, event: probeOnlineEvent, response: `{"ok": true}`,
		config: `{"slack_bot_token": "xoxb-token", "slack_channel": "#alerts", "slack_rich_message": true}`},

	{name: "teams_down", sender: teams, event: downEvent,
		config: `{"webhook_url": "https://example.powerplatform.com/workflows/1?sig=s"}`},
	{name: "teams_certificate_expiry", sender: teams, event: certExpiryEvent,
		config: `{"webhook_url": "https://example.powerplatform.com/workflows/1?sig=s", "server_url": "https://status.example.com"}`},
	{name: "teams_probe_offline", sender: teams, event: probeOfflineEvent,
		config: `{"webhook_url": "https://example.powerplatform.com/workflows/1?sig=s"}`},

	{name: "telegram_down", sender: telegram, event: downEvent,
		config: `{"bot_token": "123456:bot-token", "chat_id": "-1001234"}`},
	{name: "telegram_template_up", sender: telegram, event: upEvent,
		config: `{"bot_token": "123456:bot-token", "chat_id": "-1001234", "message_thread_id": "7", "server_url": "https://telegram.example.com", "use_template": true, "template_parse_mode": "HTML", "template": "<b>{{ name }}</b> is {{ status }}", "send_silently": true, "protect_content": true}`},
	{name: "telegram_certificate_expiry", sender: telegram, event: certExpiryEvent,
		config: `{"bot_token": "123456:bot-token", "chat_id": "-1001234"}`},

	{name: "twilio_down", sender: twilio, event: downEvent,
		config: `{"twilio_account_sid": "AC123", "twilio_auth_token": "auth-token", "twilio_from_number": "+15550000001", "twilio_to_number": "+15550000002"}`},
	{name: "twilio_api_key_up", sender: twilio, event: upEvent,
		config: `{"twilio_account_sid": "AC123", "twilio_api_key": "SK123", "twilio_auth_token": "api-secret", "twilio_from_number": "+15550000001", "twilio_to_number": "+15550000002"}`},
	{name: "twilio_certificate_expiry", sender: twilio, event: certExpiryEvent,
		config: `{"twilio_account_sid": "AC123", "twilio_auth_token": "auth-token", "twilio_from_number": "+15550000001", "twilio_to_number": "+15550000002"}`},

	{name: "webhook_json_down", sender: webhook, event: downEvent,
		config: `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json"}`},
	{name: "webhook_form_data_up", sender: webhook, event: upEvent,
		config: `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "form-data"}`},
	{name: "webhook_custom_body_down", sender: webhook, event: downEvent,
		config: `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "custom", "webhook_custom_body": "{\"text\": \"{{ name }} is {{ status }}: {{ msg }}\", \"url\": \"{{ monitor.config.url }}\"}", "webhook_additional_headers": "{\"Authorization\": \"Bearer hook-token\", \"X-Priority\": 1}"}`},
	{name: "webhook_json_certificate_expiry", sender: webhook, event: certExpiryEvent,
		config: `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json"}`},
	// The stand-in monitor's status says the probe is Up again
	{name: "webhook_json_probe_online", sender: webhook, event: probeOnlineEvent,
		config: `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json"}`},

	{name: "wecom_down", sender: weCom, event: downEvent,
		config: `{"webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k"}`},
	{name: "wecom_certificate_expiry", sender: weCom, event: certExpiryEvent,
		config: `{"webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k"}`},

	{name: "whatsapp_down", sender: whatsApp, event: downEvent,
		config: `{"server_url": "https://waha.example.com/", "api_key": "waha-key", "phone_number": "+1 (555) 000-0002", "session": "default"}`},
	{name: "whatsapp_template_up", sender: whatsApp, event: upEvent,
		config: `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default", "use_template": true, "template": "{{ status_icon }} {{ name }} is {{ status }}"}`},
	{name: "whatsapp_certificate_expiry", sender: whatsApp, event: certExpiryEvent,
		config: `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`},
}

// TestSendGoldenRequests sends each case's notification and compares the HTTP requests the
// channel makes with its golden file. Run with -update to rewrite the golden files.
func TestSendGoldenRequests(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				response := tc.response
				if response == "" {
					response = "{}"
				}
				_, _ = io.WriteString(w, response)
			}))
			defer endpoint.Close()
			transport := &recordingTransport{endpoint: endpoint}

			sender := tc.sender(&http.Client{Transport: transport})
			message, mon, hb := goldenNotification(tc.event)
			require.NoError(t, sender.Validate(tc.config), "the case's config must pass validation, as the listener checks it first")
			require.NoError(t, sender.Send(context.Background(), tc.config, message, mon, hb))

			assertGolden(t, tc.name, transport.String())
		})
	}
}

// TestSendStopsWhenContextIsDone checks that each channel makes its requests with the send's
// context, so the listener's deadline stops a send to an endpoint that never answers
func TestSendStopsWhenContextIsDone(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			var reached atomic.Bool
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached.Store(true)
			}))
			defer endpoint.Close()
			transport := &recordingTransport{endpoint: endpoint}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			message, mon, hb := goldenNotification(tc.event)
			err := tc.sender(&http.Client{Transport: transport}).Send(ctx, tc.config, message, mon, hb)

			assert.False(t, reached.Load(), "the request must not reach the endpoint once the context is done")
			if len(transport.requests) > 0 {
				assert.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}

// TestSendWithoutAMonitor checks that no channel type dereferences a monitor it isn't given,
// whether the notification carries a heartbeat or not: it sends the message, or returns an
// error (Opsgenie can't close the alert of a monitor it can't name), but doesn't panic
func TestSendWithoutAMonitor(t *testing.T) {
	heartbeats := []struct {
		name string
		hb   *heartbeat.Model
	}{
		{"no heartbeat", nil},
		{"down heartbeat", goldenDown},
		{"up heartbeat", goldenUp},
	}
	for _, tc := range goldenCases {
		for _, withHeartbeat := range heartbeats {
			t.Run(tc.name+"/"+withHeartbeat.name, func(t *testing.T) {
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					response := tc.response
					if response == "" {
						response = "{}"
					}
					_, _ = io.WriteString(w, response)
				}))
				defer endpoint.Close()
				transport := &recordingTransport{endpoint: endpoint}
				sender := tc.sender(&http.Client{Transport: transport})

				require.NotPanics(t, func() {
					_ = sender.Send(context.Background(), tc.config, "A message with no monitor", nil, withHeartbeat.hb)
				})
			})
		}
	}
}

func TestEveryGoldenFileHasACase(t *testing.T) {
	var names []string
	for _, tc := range slices.Concat(goldenCases, emailGoldenCases) {
		names = append(names, tc.name+".golden")
	}
	files, err := os.ReadDir(filepath.Join("testdata", "requests"))
	require.NoError(t, err)

	for _, file := range files {
		assert.Contains(t, names, file.Name(), "no case writes this golden file; delete it")
	}
}

// goldenNotification returns the message, monitor and heartbeat the listener sends for an event
func goldenNotification(event goldenEvent) (string, *monitor.Model, *heartbeat.Model) {
	switch event {
	case upEvent:
		return goldenUp.Msg, goldenMonitor, goldenUp
	case certExpiryEvent:
		return goldenCertMessage, goldenMonitor, nil
	case probeOfflineEvent:
		return goldenProbeOfflineMessage, goldenProbeOffline, nil
	case probeOnlineEvent:
		return goldenProbeOnlineMessage, goldenProbeOnline, nil
	default:
		return goldenDown.Msg, goldenMonitor, goldenDown
	}
}

// recordingTransport records each request a channel makes, then forwards it to the fake
// endpoint, whatever host the request was addressed to
type recordingTransport struct {
	endpoint *httptest.Server
	requests []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	rt.requests = append(rt.requests, formatRequest(req, body))

	forwarded := req.Clone(req.Context())
	forwarded.URL.Scheme = "http"
	forwarded.URL.Host = rt.endpoint.Listener.Addr().String()
	forwarded.Body = io.NopCloser(bytes.NewReader(body))
	return rt.endpoint.Client().Transport.RoundTrip(forwarded)
}

// String returns the recorded requests as they're written to a golden file
func (rt *recordingTransport) String() string {
	if len(rt.requests) == 0 {
		return "(no request)\n"
	}
	return strings.Join(rt.requests, "\n")
}

// goldenScrubs replace the parts of a request that change on every send
var goldenScrubs = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// The app version is in User-Agent headers
	{regexp.MustCompile(regexp.QuoteMeta("/" + version.Version)), "/<version>"},
	// Matrix picks a random transaction ID for each message
	{regexp.MustCompile(`/send/m\.room\.message/[A-Za-z0-9_-]{20}\n`), "/send/m.room.message/<txn-id>\n"},
	// PagerTree alert IDs end with the send time
	{regexp.MustCompile(`"Id": "monitor-mon-1-\d+"`), `"Id": "monitor-mon-1-<unix-time>"`},
}

// formatRequest writes a request as its method and URL, its headers sorted by name, and its
// body. JSON bodies are indented.
func formatRequest(req *http.Request, body []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", req.Method, req.URL.String())

	names := make([]string, 0, len(req.Header))
	for name := range req.Header {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range req.Header[name] {
			fmt.Fprintf(&b, "%s: %s\n", name, value)
		}
	}
	b.WriteString("\n")

	var indented bytes.Buffer
	if json.Indent(&indented, body, "", "  ") == nil {
		body = indented.Bytes()
	}
	b.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		b.WriteString("\n")
	}

	out := b.String()
	// A multipart body's boundary is random
	if _, params, err := mime.ParseMediaType(req.Header.Get("Content-Type")); err == nil && params["boundary"] != "" {
		out = strings.ReplaceAll(out, params["boundary"], "<boundary>")
	}
	for _, scrub := range goldenScrubs {
		out = scrub.pattern.ReplaceAllString(out, scrub.replacement)
	}
	return out
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "requests", name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run the test with -update to write it")
	assert.Equal(t, string(want), got)
}
