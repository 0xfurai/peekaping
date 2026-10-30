package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// sendFailure is a notification a channel fails to send, and the error it returns
type sendFailure struct {
	name   string
	sender func(client *http.Client) goldenSender
	config string
	event  goldenEvent
	// notify is the notification of a case that none of the events is, and replaces the event
	notify goldenNotify
	// status and body are what the fake endpoint answers with; the status is 200 when left out
	status int
	body   string
	// transportError makes the request fail before the endpoint answers
	transportError bool
	// wantErr is what the error Send returns contains
	wantErr string
}

// badURL is a URL that url.Parse refuses, so that a request to it can't be made
const badURL = `http://exa\u007fmple.com/hook`

// failingTransport fails every request, as a network that is down does
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset by peer")
}

var sendFailures = []sendFailure{
	{name: "discord config that is not JSON", sender: discord, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "discord thread with a webhook URL that can't be parsed", sender: discord,
		config:  `{"webhook_url": "http://[::1", "message_type": "send_to_thread", "thread_id": "456"}`,
		wantErr: `invalid webhook URL: parse "http://[::1": missing ']' in host`},
	{name: "discord request that can't be made", sender: discord,
		config:  `{"webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse",
	},
	{name: "discord endpoint that is down", sender: discord, transportError: true,
		config:  `{"webhook_url": "https://discord.com/api/webhooks/123/token"}`,
		wantErr: "failed to send Discord webhook: Post \"https://discord.com/api/webhooks/123/token\": connection reset by peer"},
	{name: "discord status that is not a success", sender: discord, status: 500,
		config:  `{"webhook_url": "https://discord.com/api/webhooks/123/token"}`,
		wantErr: "Discord webhook returned status: 500 Internal Server Error"},

	{name: "google chat config that is not JSON", sender: googleChat, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "google chat request that can't be made", sender: googleChat,
		config:  `{"webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "google chat endpoint that is down", sender: googleChat, transportError: true,
		config:  `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages"}`,
		wantErr: "failed to send request: Post \"https://chat.googleapis.com/v1/spaces/AAA/messages\": connection reset by peer"},
	{name: "google chat status that is not a success", sender: googleChat, status: 403,
		config:  `{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages"}`,
		wantErr: "Google Chat API returned status code: 403"},

	{name: "gotify config that is not JSON", sender: gotify, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "gotify request that can't be made", sender: gotify,
		config:  `{"server_url": "` + badURL + `", "application_token": "app-token"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "gotify endpoint that is down", sender: gotify, transportError: true,
		config:  `{"server_url": "https://gotify.example.com", "application_token": "app-token"}`,
		wantErr: "failed to send Gotify request: Post \"https://gotify.example.com/message?token=app-token\": connection reset by peer"},
	{name: "gotify status that is not a success", sender: gotify, status: 401,
		config:  `{"server_url": "https://gotify.example.com", "application_token": "app-token"}`,
		wantErr: "Gotify request failed with status 401"},

	{name: "line config that is not JSON", sender: line, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "line template that can't be rendered", sender: line,
		config:  `{"channel_access_token": "line-token", "user_id": "U123", "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "line endpoint that is down", sender: line, transportError: true,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "failed to send LINE message: Post \"https://api.line.me/v2/bot/message/push\": connection reset by peer"},
	{name: "line error with the reason the API gives", sender: line, status: 401, body: `{"message": "Authentication failed"}`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API error: Authentication failed (status: 401)"},
	{name: "line error with a body that has no reason", sender: line, status: 500, body: `{"details": []}`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API returned status: 500 Internal Server Error"},
	{name: "line error with a body that is not JSON", sender: line, status: 502, body: `bad gateway`,
		config:  `{"channel_access_token": "line-token", "user_id": "U123"}`,
		wantErr: "LINE API returned status: 502 Bad Gateway"},

	{name: "grafana oncall config that is not JSON", sender: grafanaOncall, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "grafana oncall request that can't be made", sender: grafanaOncall,
		config:  `{"grafana_oncall_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "grafana oncall endpoint that is down", sender: grafanaOncall, transportError: true,
		config:  `{"grafana_oncall_url": "https://oncall.example.com/hook"}`,
		wantErr: "failed to send request: Post \"https://oncall.example.com/hook\": connection reset by peer"},
	{name: "grafana oncall status that is not a success", sender: grafanaOncall, status: 500, body: "boom",
		config:  `{"grafana_oncall_url": "https://oncall.example.com/hook"}`,
		wantErr: "grafana OnCall API returned status 500: boom"},

	{name: "pagerduty config that is not JSON", sender: pagerDuty, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "pagerduty request that can't be made", sender: pagerDuty,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "` + badURL + `"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "pagerduty endpoint that is down", sender: pagerDuty, transportError: true,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`,
		wantErr: "failed to send PagerDuty notification: Post \"https://events.pagerduty.com/v2/enqueue\": connection reset by peer"},
	{name: "pagerduty status that is not a success", sender: pagerDuty, status: 400,
		config:  `{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"}`,
		wantErr: "PagerDuty notification failed with status code: 400"},

	{name: "pagertree config that is not JSON", sender: pagerTree, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "pagertree request that can't be made", sender: pagerTree,
		config:  `{"integrationUrl": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "pagertree endpoint that is down", sender: pagerTree, transportError: true,
		config:  `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`,
		wantErr: "failed to send PagerTree notification: Post \"https://api.pagertree.com/integration/int_123\": connection reset by peer"},
	{name: "pagertree status that is not a success", sender: pagerTree, status: 403, body: "denied",
		config:  `{"integrationUrl": "https://api.pagertree.com/integration/int_123"}`,
		wantErr: "PagerTree API returned status 403: denied"},

	{name: "pushbullet config that is not JSON", sender: pushbullet, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "pushbullet endpoint that is down", sender: pushbullet, transportError: true,
		config:  `{"pushbullet_access_token": "o.pushbullet"}`,
		wantErr: "failed to send Pushbullet notification: Post \"https://api.pushbullet.com/v2/pushes\": connection reset by peer"},
	{name: "pushbullet error with the reason the API gives", sender: pushbullet, status: 401, body: `{"error": {"message": "Access token is invalid"}}`,
		config:  `{"pushbullet_access_token": "o.pushbullet"}`,
		wantErr: "pushbullet API error: Access token is invalid (status: 401)"},
	{name: "pushbullet error with a reason that is not a message", sender: pushbullet, status: 400, body: `{"error": {"code": "invalid"}}`,
		config:  `{"pushbullet_access_token": "o.pushbullet"}`,
		wantErr: "pushbullet API returned status: 400"},
	{name: "pushbullet error with a reason that is not an object", sender: pushbullet, status: 403, body: `{"error": "forbidden"}`,
		config:  `{"pushbullet_access_token": "o.pushbullet"}`,
		wantErr: "pushbullet API returned status: 403"},
	{name: "pushbullet error with a body that is not JSON", sender: pushbullet, status: 500, body: `oops`,
		config:  `{"pushbullet_access_token": "o.pushbullet"}`,
		wantErr: "pushbullet API returned status: 500"},

	{name: "pushover config that is not JSON", sender: pushover, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "pushover endpoint that is down", sender: pushover, transportError: true,
		config:  `{"pushover_user_key": "user-key", "pushover_app_token": "app-token"}`,
		wantErr: "failed to send Pushover request: Post \"https://api.pushover.net/1/messages.json\": connection reset by peer"},
	{name: "pushover status that is not a success", sender: pushover, status: 400,
		config:  `{"pushover_user_key": "user-key", "pushover_app_token": "app-token"}`,
		wantErr: "Pushover API returned status code: 400"},

	{name: "sendgrid config that is not JSON", sender: sendGrid, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "sendgrid endpoint that is down", sender: sendGrid, transportError: true,
		config:  `{"api_key": "SG.key", "from_email": "alerts@example.com", "to_email": "oncall@example.com"}`,
		wantErr: "failed to send SendGrid email: Post \"https://api.sendgrid.com/v3/mail/send\": connection reset by peer"},
	{name: "sendgrid status that is not a success", sender: sendGrid, status: 401, body: `{"errors":[{"message":"bad key"}]}`,
		config:  `{"api_key": "SG.key", "from_email": "alerts@example.com", "to_email": "oncall@example.com"}`,
		wantErr: `SendGrid API error: 401 - {"errors":[{"message":"bad key"}]}`},

	{name: "mattermost config that is not JSON", sender: mattermost, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "mattermost template that can't be rendered for a heartbeat", sender: mattermost, event: downEvent,
		config:  `{"webhook_url": "https://mattermost.example.com/hooks/abc", "use_template": true, "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "mattermost template that can't be rendered without a heartbeat", sender: mattermost, event: certExpiryEvent,
		config:  `{"webhook_url": "https://mattermost.example.com/hooks/abc", "use_template": true, "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "mattermost request that can't be made", sender: mattermost, event: downEvent,
		config:  `{"webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "mattermost endpoint that is down", sender: mattermost, transportError: true, event: downEvent,
		config:  `{"webhook_url": "https://mattermost.example.com/hooks/abc"}`,
		wantErr: "failed to send request: Post \"https://mattermost.example.com/hooks/abc\": connection reset by peer"},
	{name: "mattermost status that is not a success", sender: mattermost, status: 500, event: downEvent,
		config:  `{"webhook_url": "https://mattermost.example.com/hooks/abc"}`,
		wantErr: "Mattermost API returned status 500"},

	{name: "ntfy config that is not JSON", sender: ntfy, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "ntfy request that can't be made", sender: ntfy,
		config:  `{"server_url": "` + badURL + `", "topic": "peekaping", "authentication_type": "none"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "ntfy basic authentication without a user name", sender: ntfy,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "basic", "password": "ntfy-pass"}`,
		wantErr: "username and password required for basic authentication"},
	{name: "ntfy basic authentication without a password", sender: ntfy,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "basic", "username": "ntfy-user"}`,
		wantErr: "username and password required for basic authentication"},
	{name: "ntfy token authentication without a token", sender: ntfy,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "token"}`,
		wantErr: "token required for token authentication"},
	{name: "ntfy authentication of a type it doesn't know", sender: ntfy,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "digest"}`,
		wantErr: "unsupported authentication type: digest"},
	{name: "ntfy authentication of no type", sender: ntfy,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping"}`,
		wantErr: "unsupported authentication type: "},
	{name: "ntfy endpoint that is down", sender: ntfy, transportError: true,
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "none"}`,
		wantErr: "failed to send NTFY request: Post \"https://ntfy.example.com/peekaping\": connection reset by peer"},
	{name: "ntfy status that is not a success", sender: ntfy, status: 429, body: "rate limited",
		config:  `{"server_url": "https://ntfy.example.com", "topic": "peekaping", "authentication_type": "none"}`,
		wantErr: "NTFY request failed with status 429: rate limited"},

	{name: "slack config that is not JSON", sender: slack, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "slack template that can't be rendered", sender: slack,
		config:  `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X", "use_template": true, "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "slack request that can't be made", sender: slack,
		config:  `{"slack_webhook_url": "` + badURL + `"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "slack endpoint that is down leaves the webhook URL out of the error", sender: slack, transportError: true,
		config:  `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X"}`,
		wantErr: "failed to send Slack message: connection reset by peer"},
	{name: "slack webhook status with an error code", sender: slack, status: 404, body: "no_service",
		config:  `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X"}`,
		wantErr: "Slack webhook returned status 404 Not Found: no_service"},
	{name: "slack webhook status with a body that is not an error code", sender: slack, status: 500, body: "<html>oops</html>",
		config:  `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X"}`,
		wantErr: "Slack webhook returned status 500 Internal Server Error"},
	{name: "slack API status that is not a success", sender: slack, status: 500, body: "<html>oops</html>",
		config:  `{"slack_bot_token": "xoxb-token", "slack_channel": "#alerts"}`,
		wantErr: "Slack API returned status 500 Internal Server Error: <html>oops</html>"},
	{name: "slack API response that is not JSON", sender: slack, body: "not json",
		config:  `{"slack_bot_token": "xoxb-token", "slack_channel": "#alerts"}`,
		wantErr: "failed to decode Slack API response: "},
	{name: "slack API response that says it failed", sender: slack, body: `{"ok": false, "error": "channel_not_found"}`,
		config:  `{"slack_bot_token": "xoxb-token", "slack_channel": "#alerts"}`,
		wantErr: "Slack API error: channel_not_found"},

	{name: "telegram config that is not JSON", sender: telegram, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "telegram template that can't be rendered", sender: telegram, event: downEvent,
		config:  `{"bot_token": "123456:bot-token", "chat_id": "-1001234", "use_template": true, "template": "{{ name | nofilter }}"}`,
		wantErr: "failed to render template: "},
	{name: "telegram request that can't be made", sender: telegram, event: downEvent,
		config:  `{"bot_token": "123456:bot-token", "chat_id": "-1001234", "server_url": "` + badURL + `"}`,
		wantErr: "failed to create request: parse"},
	{name: "telegram endpoint that is down", sender: telegram, transportError: true, event: downEvent,
		config:  `{"bot_token": "123456:bot-token", "chat_id": "-1001234"}`,
		wantErr: "failed to send telegram message: Get "},
	{name: "telegram status that is not a success", sender: telegram, status: 400, event: downEvent, body: `{"ok":false,"description":"Bad Request: chat not found"}`,
		config:  `{"bot_token": "123456:bot-token", "chat_id": "-1001234"}`,
		wantErr: `telegram API returned status: 400 Bad Request, body: {"ok":false,"description":"Bad Request: chat not found"}`},

	{name: "webhook config that is not JSON", sender: webhook, config: `{`,
		wantErr: "failed to parse config: unexpected end of JSON input"},
	{name: "webhook content type it doesn't know", sender: webhook, event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "xml"}`,
		wantErr: "unsupported content type: xml"},
	{name: "webhook custom body that is left out", sender: webhook, event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "custom"}`,
		wantErr: "custom body is required when content type is custom"},
	{name: "webhook custom body that can't be rendered", sender: webhook, event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "custom", "webhook_custom_body": "{{ name | nofilter }}"}`,
		wantErr: "failed to render custom body template: "},
	{name: "webhook request that can't be made", sender: webhook, event: downEvent,
		config:  `{"webhook_url": "` + badURL + `", "webhook_content_type": "json"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "webhook endpoint that is down", sender: webhook, transportError: true, event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json"}`,
		wantErr: "failed to send webhook request: Post \"https://hooks.example.com/peekaping\": connection reset by peer"},
	{name: "webhook endpoint that is down when the request is signed", sender: webhook, transportError: true, event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json", "webhook_signing_secret": "s3cret"}`,
		wantErr: "failed to send webhook request: Post \"https://hooks.example.com/peekaping\": connection reset by peer"},
	{name: "webhook status that is not a success", sender: webhook, status: 500, body: "oops", event: downEvent,
		config:  `{"webhook_url": "https://hooks.example.com/peekaping", "webhook_content_type": "json"}`,
		wantErr: "webhook returned status 500: oops"},

	{name: "whatsapp config that is not JSON", sender: whatsApp, config: `{`,
		wantErr: "failed to unmarshal config: failed to parse config: unexpected end of JSON input"},
	{name: "whatsapp request that can't be made", sender: whatsApp, event: downEvent,
		config:  `{"server_url": "` + badURL + `", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "failed to create HTTP request: parse"},
	{name: "whatsapp endpoint that is down", sender: whatsApp, transportError: true, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "failed to send HTTP request: Post \"https://waha.example.com/api/sendText\": connection reset by peer"},
	{name: "whatsapp error with the reason the server gives", sender: whatsApp, status: 422, body: `{"message": "Session not found"}`, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "WAHA API error: Session not found (status: 422)"},
	{name: "whatsapp error with a body that has no reason", sender: whatsApp, status: 500, body: `{"error": "boom"}`, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "WAHA API returned status: 500"},
	{name: "whatsapp error with a body that is not JSON", sender: whatsApp, status: 502, body: `bad gateway`, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "WAHA API returned status: 502"},
	{name: "whatsapp success that says it failed", sender: whatsApp, body: `{"success": false, "message": "number not on WhatsApp"}`, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "WAHA API reported failure: number not on WhatsApp"},
	{name: "whatsapp success that says it failed without a reason", sender: whatsApp, body: `{"success": false}`, event: downEvent,
		config:  `{"server_url": "https://waha.example.com", "phone_number": "15550000002", "session": "default"}`,
		wantErr: "WAHA API reported failure"},
}

// TestSendFailures checks the error each channel returns when it can't send a notification
func TestSendFailures(t *testing.T) {
	for _, tc := range sendFailures {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer endpoint.Close()

			client := &http.Client{Transport: &recordingTransport{endpoint: endpoint}}
			if tc.transportError {
				client = &http.Client{Transport: failingTransport{}}
			}
			message, mon, hb := notificationOf(tc.event, tc.notify)
			err := tc.sender(client).Send(context.Background(), tc.config, message, mon, hb)

			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
